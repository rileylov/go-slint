//go:build goslint_android

// Android entry + the real JNI notification code. Everything here uses only the
// NDK's jni.h and Android framework classes (no app dex, no Java build step) —
// which is why it works inside goslint's hasCode="false" APK: the JVM already
// runs in the process, FindClass resolves framework classes from any attached
// thread, and the small-icon requirement is met with a runtime bitmap instead of
// an APK resource. Debug with: adb logcat -s goslint-notify goslintapp
package main

/*
#cgo LDFLAGS: -llog
#include <android/log.h>
#include <jni.h>
#include <stdint.h>
#include <stdlib.h>

#define NOTIFY_TAG "goslint-notify"

static void notify_log(const char *msg) {
	__android_log_write(ANDROID_LOG_INFO, NOTIFY_TAG, msg);
}

// notify_env returns a JNIEnv for this OS thread, attaching it to the VM when
// needed. *attached reports whether WE attached (then the caller detaches — a
// thread the framework attached must be left alone).
static JNIEnv *notify_env(JavaVM *vm, int *attached) {
	JNIEnv *env = NULL;
	*attached = 0;
	jint r = (*vm)->GetEnv(vm, (void **)&env, JNI_VERSION_1_6);
	if (r == JNI_OK) {
		return env;
	}
	if (r == JNI_EDETACHED && (*vm)->AttachCurrentThread(vm, &env, NULL) == JNI_OK) {
		*attached = 1;
		return env;
	}
	return NULL;
}

// notify_exc logs+clears a pending Java exception (leaving one pending makes the
// next JNI call fatal). Returns 1 if one was pending.
static int notify_exc(JNIEnv *env) {
	if ((*env)->ExceptionCheck(env)) {
		(*env)->ExceptionDescribe(env); // goes to logcat
		(*env)->ExceptionClear(env);
		return 1;
	}
	return 0;
}

static jint notify_sdk_int(JNIEnv *env) {
	jclass ver = (*env)->FindClass(env, "android/os/Build$VERSION");
	if (notify_exc(env) || !ver) return -1;
	jfieldID f = (*env)->GetStaticFieldID(env, ver, "SDK_INT", "I");
	if (notify_exc(env) || !f) return -1;
	return (*env)->GetStaticIntField(env, ver, f);
}

// ensure_notify_permission: 1 = granted, 0 = system dialog shown (grant, then
// retry), negative = the failing step. POST_NOTIFICATIONS only exists on
// Android 13+ (API 33); before that notifications need no runtime permission.
static int ensure_notify_permission(uintptr_t vm_p, uintptr_t act_p) {
	JavaVM *vm = (JavaVM *)vm_p;
	jobject act = (jobject)act_p;
	int attached, ret = -1;
	JNIEnv *env = notify_env(vm, &attached);
	if (!env) return -1;

	if (notify_sdk_int(env) < 33) {
		ret = 1;
		goto out;
	}
	{
		jclass cls = (*env)->GetObjectClass(env, act);
		jmethodID check = (*env)->GetMethodID(env, cls, "checkSelfPermission", "(Ljava/lang/String;)I");
		if (notify_exc(env) || !check) { ret = -2; goto out; }
		jstring perm = (*env)->NewStringUTF(env, "android.permission.POST_NOTIFICATIONS");
		jint st = (*env)->CallIntMethod(env, act, check, perm);
		if (notify_exc(env)) { ret = -3; goto out; }
		if (st == 0) { ret = 1; goto out; } // PERMISSION_GRANTED

		jmethodID req = (*env)->GetMethodID(env, cls, "requestPermissions", "([Ljava/lang/String;I)V");
		if (notify_exc(env) || !req) { ret = -4; goto out; }
		jobjectArray arr = (*env)->NewObjectArray(env, 1, (*env)->FindClass(env, "java/lang/String"), perm);
		if (notify_exc(env) || !arr) { ret = -5; goto out; }
		(*env)->CallVoidMethod(env, act, req, arr, 7261);
		if (notify_exc(env)) { ret = -6; goto out; }
		ret = 0;
	}
out:
	if (attached) (*vm)->DetachCurrentThread(vm);
	return ret;
}

// send_notification posts a notification through NotificationManager. Returns 0
// on success, else the number of the step that failed (each step's Java
// exception, if any, is in logcat). Needs API 26+ (NotificationChannel).
static int send_notification(uintptr_t vm_p, uintptr_t act_p, const char *title, const char *text, int id) {
	JavaVM *vm = (JavaVM *)vm_p;
	jobject act = (jobject)act_p;
	int attached, step = 0;
	JNIEnv *env = notify_env(vm, &attached);
	if (!env) return 1;

#define STEP(n, bad) do { if (notify_exc(env) || (bad)) { step = (n); goto out; } } while (0)

	// nm = (NotificationManager) activity.getSystemService("notification")
	jclass actCls = (*env)->GetObjectClass(env, act);
	jmethodID gss = (*env)->GetMethodID(env, actCls, "getSystemService", "(Ljava/lang/String;)Ljava/lang/Object;");
	STEP(2, !gss);
	jobject nm = (*env)->CallObjectMethod(env, act, gss, (*env)->NewStringUTF(env, "notification"));
	STEP(3, !nm);

	// Idempotent: new NotificationChannel("goslint.demo", "goslint demo", IMPORTANCE_DEFAULT=3)
	jclass chCls = (*env)->FindClass(env, "android/app/NotificationChannel");
	STEP(4, !chCls);
	jmethodID chCtor = (*env)->GetMethodID(env, chCls, "<init>", "(Ljava/lang/String;Ljava/lang/CharSequence;I)V");
	STEP(5, !chCtor);
	jstring chId = (*env)->NewStringUTF(env, "goslint.demo");
	jobject ch = (*env)->NewObject(env, chCls, chCtor, chId, (*env)->NewStringUTF(env, "goslint demo"), 3);
	STEP(6, !ch);
	jclass nmCls = (*env)->GetObjectClass(env, nm);
	jmethodID createCh = (*env)->GetMethodID(env, nmCls, "createNotificationChannel", "(Landroid/app/NotificationChannel;)V");
	STEP(7, !createCh);
	(*env)->CallVoidMethod(env, nm, createCh, ch);
	STEP(8, 0);

	// Small icon (mandatory) from a runtime bitmap — the APK ships no resources.
	jclass bmpCls = (*env)->FindClass(env, "android/graphics/Bitmap");
	STEP(9, !bmpCls);
	jclass cfgCls = (*env)->FindClass(env, "android/graphics/Bitmap$Config");
	STEP(10, !cfgCls);
	jfieldID argb = (*env)->GetStaticFieldID(env, cfgCls, "ARGB_8888", "Landroid/graphics/Bitmap$Config;");
	STEP(11, !argb);
	jobject cfg = (*env)->GetStaticObjectField(env, cfgCls, argb);
	jmethodID mkBmp = (*env)->GetStaticMethodID(env, bmpCls, "createBitmap", "(IILandroid/graphics/Bitmap$Config;)Landroid/graphics/Bitmap;");
	STEP(12, !mkBmp);
	jobject bmp = (*env)->CallStaticObjectMethod(env, bmpCls, mkBmp, 64, 64, cfg);
	STEP(13, !bmp);
	jmethodID erase = (*env)->GetMethodID(env, bmpCls, "eraseColor", "(I)V");
	STEP(14, !erase);
	(*env)->CallVoidMethod(env, bmp, erase, (jint)0xFF2F6FED);
	STEP(15, 0);
	jclass iconCls = (*env)->FindClass(env, "android/graphics/drawable/Icon");
	STEP(16, !iconCls);
	jmethodID cwb = (*env)->GetStaticMethodID(env, iconCls, "createWithBitmap", "(Landroid/graphics/Bitmap;)Landroid/graphics/drawable/Icon;");
	STEP(17, !cwb);
	jobject icon = (*env)->CallStaticObjectMethod(env, iconCls, cwb, bmp);
	STEP(18, !icon);

	// new Notification.Builder(activity, channelId).setSmallIcon(...).setContentTitle(...).setContentText(...).build()
	jclass bCls = (*env)->FindClass(env, "android/app/Notification$Builder");
	STEP(19, !bCls);
	jmethodID bCtor = (*env)->GetMethodID(env, bCls, "<init>", "(Landroid/content/Context;Ljava/lang/String;)V");
	STEP(20, !bCtor);
	jobject b = (*env)->NewObject(env, bCls, bCtor, act, chId);
	STEP(21, !b);
	jmethodID mIcon = (*env)->GetMethodID(env, bCls, "setSmallIcon", "(Landroid/graphics/drawable/Icon;)Landroid/app/Notification$Builder;");
	STEP(22, !mIcon);
	(*env)->CallObjectMethod(env, b, mIcon, icon);
	STEP(23, 0);
	jmethodID mTitle = (*env)->GetMethodID(env, bCls, "setContentTitle", "(Ljava/lang/CharSequence;)Landroid/app/Notification$Builder;");
	STEP(24, !mTitle);
	(*env)->CallObjectMethod(env, b, mTitle, (*env)->NewStringUTF(env, title));
	STEP(25, 0);
	jmethodID mText = (*env)->GetMethodID(env, bCls, "setContentText", "(Ljava/lang/CharSequence;)Landroid/app/Notification$Builder;");
	STEP(26, !mText);
	(*env)->CallObjectMethod(env, b, mText, (*env)->NewStringUTF(env, text));
	STEP(27, 0);
	jmethodID build = (*env)->GetMethodID(env, bCls, "build", "()Landroid/app/Notification;");
	STEP(28, !build);
	jobject n = (*env)->CallObjectMethod(env, b, build);
	STEP(29, !n);

	jmethodID doNotify = (*env)->GetMethodID(env, nmCls, "notify", "(ILandroid/app/Notification;)V");
	STEP(30, !doNotify);
	(*env)->CallVoidMethod(env, nm, doNotify, id, n);
	STEP(31, 0);
	notify_log("notification posted");
#undef STEP
out:
	if (attached) (*vm)->DetachCurrentThread(vm);
	return step;
}
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"

	"github.com/rileylov/go-slint"
)

//export goslint_android_main
func goslint_android_main(_ *C.char) {
	runtime.LockOSThread() // Slint is thread-affine
	if err := run("material"); err != nil {
		msg := C.CString("run: " + err.Error())
		C.notify_log(msg)
		C.free(unsafe.Pointer(msg))
	}
}

func main() {} // required for c-shared; unused on android

// ensureNotificationPermission asks Android for POST_NOTIFICATIONS (13+). The
// first call typically pops the system dialog and returns false — tap again
// after granting.
func ensureNotificationPermission() (bool, error) {
	r := C.ensure_notify_permission(C.uintptr_t(slint.AndroidJavaVM()), C.uintptr_t(slint.AndroidActivity()))
	switch {
	case r == 1:
		return true, nil
	case r == 0:
		return false, nil
	default:
		return false, fmt.Errorf("JNI step %d (see: adb logcat -s goslint-notify)", int(r))
	}
}

// sendNotification posts a real system notification through NotificationManager.
func sendNotification(title, text string, id int) error {
	ct := C.CString(title)
	defer C.free(unsafe.Pointer(ct))
	cx := C.CString(text)
	defer C.free(unsafe.Pointer(cx))
	if step := C.send_notification(C.uintptr_t(slint.AndroidJavaVM()), C.uintptr_t(slint.AndroidActivity()), ct, cx, C.int(id)); step != 0 {
		return fmt.Errorf("JNI step %d failed (see: adb logcat -s goslint-notify)", int(step))
	}
	return nil
}
