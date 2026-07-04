// Type introspection for `goslint generate` (typed codegen). Walks the rich
// `i_slint_compiler::langtype::Type` exposed via the interpreter's
// properties_and_callbacks() / global_properties_and_callbacks() and emits the
// component's interface as JSON, which the Go generator consumes.
//
// This is the ONLY place that depends on the compiler's internal type AST; it
// affects `goslint generate` only, never the runtime binding.

use crate::{guard, to_c_string};
use i_slint_compiler::langtype::{Function, Struct as LangStruct, Type as LangType};
use i_slint_compiler::object_tree::PropertyVisibility;
use serde::Serialize;
use slint_interpreter::ComponentDefinition;
use std::collections::BTreeMap;
use std::ffi::c_char;

#[derive(Serialize)]
struct TypeInfo {
    kind: &'static str, // int float string bool color brush image array struct enum other
    #[serde(skip_serializing_if = "Option::is_none")]
    elem: Option<Box<TypeInfo>>, // array element
    #[serde(skip_serializing_if = "Option::is_none")]
    name: Option<String>, // struct/enum name
}

#[derive(Serialize)]
struct Property {
    name: String,
    ty: TypeInfo,
    // "in" | "out" | "in-out" for component/global properties; None for struct fields.
    // Output-only ("out") properties get no Go setter (setting one fails at runtime).
    #[serde(skip_serializing_if = "Option::is_none")]
    direction: Option<String>,
}

#[derive(Serialize)]
struct Callable {
    name: String,
    args: Vec<TypeInfo>,
    arg_names: Vec<String>,
    ret: TypeInfo,
}

#[derive(Serialize, Default)]
struct StructInfo {
    fields: Vec<Property>,
}

#[derive(Serialize, Default)]
struct EnumInfo {
    values: Vec<String>,
}

#[derive(Serialize)]
struct GlobalInfo {
    name: String,
    properties: Vec<Property>,
    callbacks: Vec<Callable>,
    functions: Vec<Callable>,
}

#[derive(Serialize)]
struct Interface {
    component: String,
    properties: Vec<Property>,
    callbacks: Vec<Callable>,
    functions: Vec<Callable>,
    globals: Vec<GlobalInfo>,
    structs: BTreeMap<String, StructInfo>,
    enums: BTreeMap<String, EnumInfo>,
}

#[derive(Default)]
struct Registry {
    structs: BTreeMap<String, StructInfo>,
    enums: BTreeMap<String, EnumInfo>,
}

fn simple(kind: &'static str) -> TypeInfo {
    TypeInfo {
        kind,
        elem: None,
        name: None,
    }
}

fn type_info(ty: &LangType, reg: &mut Registry) -> TypeInfo {
    match ty {
        LangType::Void | LangType::InferredCallback => simple("void"),
        LangType::Int32 => simple("int"),
        LangType::Float32
        | LangType::Duration
        | LangType::PhysicalLength
        | LangType::LogicalLength
        | LangType::Rem
        | LangType::Angle
        | LangType::Percent
        | LangType::UnitProduct(_) => simple("float"),
        LangType::String => simple("string"),
        LangType::Bool => simple("bool"),
        LangType::Color => simple("color"),
        LangType::Brush => simple("brush"),
        LangType::Image => simple("image"),
        LangType::Array(inner) => TypeInfo {
            kind: "array",
            elem: Some(Box::new(type_info(inner, reg))),
            name: None,
        },
        LangType::Struct(s) => named_struct(s, reg),
        LangType::Enumeration(e) => {
            let name = e.name.to_string();
            reg.enums.entry(name.clone()).or_insert_with(|| EnumInfo {
                values: e.values.iter().map(|v| v.to_string()).collect(),
            });
            TypeInfo {
                kind: "enum",
                elem: None,
                name: Some(name),
            }
        }
        // anonymous structs, styled-text, path data, etc. -> dynamic fallback
        _ => simple("other"),
    }
}

fn named_struct(s: &LangStruct, reg: &mut Registry) -> TypeInfo {
    match s.name.slint_name() {
        Some(name) => {
            let name = name.to_string();
            if !reg.structs.contains_key(&name) {
                // insert a placeholder first to guard against recursive structs
                reg.structs.insert(name.clone(), StructInfo::default());
                let fields = s
                    .fields
                    .iter()
                    .map(|(k, t)| Property {
                        name: k.to_string(),
                        ty: type_info(t, reg),
                        direction: None, // struct fields have no in/out direction
                    })
                    .collect();
                reg.structs.insert(name.clone(), StructInfo { fields });
            }
            TypeInfo {
                kind: "struct",
                elem: None,
                name: Some(name),
            }
        }
        None => simple("other"), // anonymous struct -> dynamic
    }
}

fn callable(name: String, f: &Function, reg: &mut Registry) -> Callable {
    Callable {
        name,
        args: f.args.iter().map(|t| type_info(t, reg)).collect(),
        arg_names: f.arg_names.iter().map(|s| s.to_string()).collect(),
        ret: type_info(&f.return_type, reg),
    }
}

fn classify(
    name: String,
    ty: &LangType,
    vis: PropertyVisibility,
    reg: &mut Registry,
    props: &mut Vec<Property>,
    cbs: &mut Vec<Callable>,
    fns: &mut Vec<Callable>,
) {
    match ty {
        LangType::Callback(f) => cbs.push(callable(name, f, reg)),
        LangType::Function(f) => fns.push(callable(name, f, reg)),
        _ => props.push(Property {
            name,
            ty: type_info(ty, reg),
            // Only Input/InOut are settable from Go; Output (and Constexpr/Fake/…)
            // get a getter but no setter.
            direction: Some(
                match vis {
                    PropertyVisibility::Input => "in",
                    PropertyVisibility::InOut => "in-out",
                    _ => "out",
                }
                .to_string(),
            ),
        }),
    }
}

/// JSON describing a component's typed interface (for `goslint generate`).
/// NULL on failure.
///
/// # Safety
/// `d` must be NULL or a definition pointer.
#[no_mangle]
pub unsafe extern "C" fn goslint_definition_type_info(
    d: *const ComponentDefinition,
) -> *mut c_char {
    guard(std::ptr::null_mut(), || {
        let def = match d.as_ref() {
            Some(d) => d,
            None => {
                crate::set_last_error("type_info: definition is NULL");
                return std::ptr::null_mut();
            }
        };
        let mut reg = Registry::default();

        let (mut props, mut cbs, mut fns) = (Vec::new(), Vec::new(), Vec::new());
        for (name, (ty, vis)) in def.properties_and_callbacks() {
            if matches!(vis, PropertyVisibility::Private) {
                continue;
            }
            classify(name, &ty, vis, &mut reg, &mut props, &mut cbs, &mut fns);
        }

        let mut globals = Vec::new();
        for g in def.globals() {
            let (mut gp, mut gc, mut gf) = (Vec::new(), Vec::new(), Vec::new());
            if let Some(items) = def.global_properties_and_callbacks(&g) {
                for (name, (ty, vis)) in items {
                    if matches!(vis, PropertyVisibility::Private) {
                        continue;
                    }
                    classify(name, &ty, vis, &mut reg, &mut gp, &mut gc, &mut gf);
                }
            }
            globals.push(GlobalInfo {
                name: g,
                properties: gp,
                callbacks: gc,
                functions: gf,
            });
        }

        let iface = Interface {
            component: def.name().to_string(),
            properties: props,
            callbacks: cbs,
            functions: fns,
            globals,
            structs: reg.structs,
            enums: reg.enums,
        };
        match serde_json::to_string(&iface) {
            Ok(s) => to_c_string(&s),
            Err(_) => std::ptr::null_mut(),
        }
    })
}
