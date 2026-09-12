/*
 * Zen remote-desktop JNI bridge for the pinned moonlight-common-c core.
 *
 * This file only translates the Zen Kotlin contract onto the upstream C API.
 * Stream setup, RTSP/ENet transport, encryption, FEC, retransmission, pairing
 * (handled by the app layer before start) and revocation stay upstream.
 *
 * Upstream lifecycle rules honored here:
 *  - LiStartConnection() blocks until the session ends and is not thread-safe.
 *    Exactly one start may be in flight; callers run it on a dedicated thread.
 *  - LiStopConnection() is not thread-safe. To stop a live session the caller
 *    first calls LiInterruptConnection() (thread-safe), then joins the start
 *    thread, and only afterwards may this bridge re-arm for a new session.
 *  - submitDecodeUnit() data is owned by the core and freed after the callback
 *    returns, so the bridge copies every buffer synchronously into a Java array.
 */
#include <jni.h>
#include <pthread.h>
#include <stdint.h>
#include <string.h>
#include <stdio.h>

#include "Limelight.h"

#ifdef __ANDROID__
#include <android/log.h>
#define BRIDGE_LOG(...) __android_log_print(ANDROID_LOG_INFO, "zen-moonlight", __VA_ARGS__)
#else
#define BRIDGE_LOG(...) fprintf(stderr, "[zen-moonlight] " __VA_ARGS__)
#endif

#define BRIDGE_ERR_BUSY -2
#define BRIDGE_ERR_STATE -3

static JavaVM *g_vm;
static jobject g_listener;
static jmethodID g_on_video_setup;
static jmethodID g_on_stage;
static jmethodID g_on_stage_failed;
static jmethodID g_on_connected;
static jmethodID g_on_terminated;
static jmethodID g_on_decode_unit;

static pthread_mutex_t g_session_lock = PTHREAD_MUTEX_INITIALIZER;
static int g_started;
static int g_stop_requested;

static JNIEnv *bridge_env(int *attached) {
    JNIEnv *env = NULL;
    *attached = 0;
    if (g_vm == NULL) {
        return NULL;
    }
    if ((*g_vm)->GetEnv(g_vm, (void **)&env, JNI_VERSION_1_6) == JNI_OK) {
        return env;
    }
#ifdef __ANDROID__
    /* The NDK JNI header declares AttachCurrentThread with a JNIEnv** out-param. */
    if ((*g_vm)->AttachCurrentThread(g_vm, &env, NULL) == JNI_OK) {
#else
    if ((*g_vm)->AttachCurrentThread(g_vm, (void **)&env, NULL) == JNI_OK) {
#endif
        *attached = 1;
        return env;
    }
    return NULL;
}

static void bridge_detach(JNIEnv *env, int attached) {
    (void)env;
    if (attached && g_vm != NULL) {
        (*g_vm)->DetachCurrentThread(g_vm);
    }
}

static void bridge_clear_exception(JNIEnv *env) {
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    }
}

/* ---- decoder renderer callbacks ---------------------------------------- */

static int bridge_video_setup(int videoFormat, int width, int height, int redrawRate, void *context, int drFlags) {
    (void)context;
    (void)drFlags;
    int attached = 0;
    JNIEnv *env = bridge_env(&attached);
    if (env == NULL || g_listener == NULL) {
        return -1;
    }
    jint result = (*env)->CallIntMethod(env, g_listener, g_on_video_setup,
                                        (jint)videoFormat, (jint)width, (jint)height, (jint)redrawRate);
    bridge_clear_exception(env);
    bridge_detach(env, attached);
    return result == 0 ? 0 : -1;
}

static void bridge_video_start(void) {
    BRIDGE_LOG("video start\n");
}

static void bridge_video_stop(void) {
    BRIDGE_LOG("video stop\n");
}

static void bridge_video_cleanup(void) {
    BRIDGE_LOG("video cleanup\n");
}

static int bridge_submit_decode_unit(PDECODE_UNIT decodeUnit) {
    int attached = 0;
    JNIEnv *env = bridge_env(&attached);
    if (env == NULL || g_listener == NULL) {
        return DR_NEED_IDR;
    }

    jbyteArray frame = (*env)->NewByteArray(env, (jsize)decodeUnit->fullLength);
    if (frame == NULL) {
        bridge_clear_exception(env);
        bridge_detach(env, attached);
        return DR_NEED_IDR;
    }

    jsize offset = 0;
    for (PLENTRY entry = decodeUnit->bufferList; entry != NULL; entry = entry->next) {
        if (entry->length <= 0) {
            continue;
        }
        (*env)->SetByteArrayRegion(env, frame, offset, (jsize)entry->length, (const jbyte *)entry->data);
        offset += (jsize)entry->length;
        if ((*env)->ExceptionCheck(env)) {
            bridge_clear_exception(env);
            (*env)->DeleteLocalRef(env, frame);
            bridge_detach(env, attached);
            return DR_NEED_IDR;
        }
    }

    jint result = (*env)->CallIntMethod(env, g_listener, g_on_decode_unit,
                                        frame, (jint)decodeUnit->frameType,
                                        (jlong)decodeUnit->presentationTimeUs);
    bridge_clear_exception(env);
    (*env)->DeleteLocalRef(env, frame);
    bridge_detach(env, attached);
    return result == DR_OK ? DR_OK : DR_NEED_IDR;
}

/* ---- connection listener callbacks ------------------------------------- */

static void bridge_stage_starting(int stage) {
    int attached = 0;
    JNIEnv *env = bridge_env(&attached);
    if (env == NULL || g_listener == NULL) {
        return;
    }
    (*env)->CallVoidMethod(env, g_listener, g_on_stage, (jint)stage);
    bridge_clear_exception(env);
    bridge_detach(env, attached);
}

static void bridge_stage_failed(int stage, int errorCode) {
    int attached = 0;
    JNIEnv *env = bridge_env(&attached);
    if (env == NULL || g_listener == NULL) {
        return;
    }
    (*env)->CallVoidMethod(env, g_listener, g_on_stage_failed, (jint)stage, (jint)errorCode);
    bridge_clear_exception(env);
    bridge_detach(env, attached);
}

static void bridge_connection_started(void) {
    int attached = 0;
    JNIEnv *env = bridge_env(&attached);
    if (env == NULL || g_listener == NULL) {
        return;
    }
    (*env)->CallVoidMethod(env, g_listener, g_on_connected);
    bridge_clear_exception(env);
    bridge_detach(env, attached);
}

static void bridge_connection_terminated(int errorCode) {
    int attached = 0;
    JNIEnv *env = bridge_env(&attached);
    pthread_mutex_lock(&g_session_lock);
    g_stop_requested = 1;
    pthread_mutex_unlock(&g_session_lock);
    if (env == NULL || g_listener == NULL) {
        return;
    }
    (*env)->CallVoidMethod(env, g_listener, g_on_terminated, (jint)errorCode);
    bridge_clear_exception(env);
    bridge_detach(env, attached);
}

/* ---- audio renderer callbacks (no audio pipeline in the prototype) ----- */

static int bridge_audio_init(int audioConfiguration, const POPUS_MULTISTREAM_CONFIGURATION opusConfig, void *context, int arFlags) {
    (void)audioConfiguration;
    (void)opusConfig;
    (void)context;
    (void)arFlags;
    return 0;
}

static void bridge_audio_start(void) {}
static void bridge_audio_stop(void) {}
static void bridge_audio_cleanup(void) {}
static void bridge_audio_decode(char *sampleData, int sampleLength) {
    (void)sampleData;
    (void)sampleLength;
}

/* ---- helpers ----------------------------------------------------------- */

static void bridge_copy_bytes(JNIEnv *env, jbyteArray source, char *destination, int length) {
    if (source == NULL) {
        return;
    }
    if ((*env)->GetArrayLength(env, source) < length) {
        return;
    }
    (*env)->GetByteArrayRegion(env, source, 0, length, (jbyte *)destination);
}

static jstring bridge_optional_string(JNIEnv *env, jstring value) {
    return value == NULL ? (*env)->NewStringUTF(env, "") : value;
}

/* ---- JNI entry points -------------------------------------------------- */

JNIEXPORT jint JNICALL JNI_OnLoad(JavaVM *vm, void *reserved) {
    (void)reserved;
    g_vm = vm;
    return JNI_VERSION_1_6;
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeStartConnection(
        JNIEnv *env, jobject thiz,
        jstring address, jstring appVersion, jstring gfeVersion, jstring rtspSessionUrl,
        jint serverCodecModeSupport, jint width, jint height, jint fps, jint bitrate,
        jint packetSize, jint audioConfiguration, jint supportedVideoFormats,
        jint clientRefreshRateX100, jint streamingRemotely,
        jbyteArray remoteInputAesKey, jbyteArray remoteInputAesIv) {
    pthread_mutex_lock(&g_session_lock);
    if (g_started) {
        pthread_mutex_unlock(&g_session_lock);
        return BRIDGE_ERR_BUSY;
    }
    if (g_listener != NULL) {
        (*env)->DeleteGlobalRef(env, g_listener);
    }
    g_listener = (*env)->NewGlobalRef(env, thiz);
    if (g_listener == NULL) {
        pthread_mutex_unlock(&g_session_lock);
        return BRIDGE_ERR_STATE;
    }
    g_started = 1;
    g_stop_requested = 0;
    pthread_mutex_unlock(&g_session_lock);

    jclass listenerClass = (*env)->GetObjectClass(env, thiz);
    g_on_video_setup = (*env)->GetMethodID(env, listenerClass, "onVideoSetup", "(IIII)I");
    g_on_stage = (*env)->GetMethodID(env, listenerClass, "onConnectionStage", "(I)V");
    g_on_stage_failed = (*env)->GetMethodID(env, listenerClass, "onConnectionStageFailed", "(II)V");
    g_on_connected = (*env)->GetMethodID(env, listenerClass, "onConnectionStarted", "()V");
    g_on_terminated = (*env)->GetMethodID(env, listenerClass, "onConnectionTerminated", "(I)V");
    g_on_decode_unit = (*env)->GetMethodID(env, listenerClass, "onDecodeUnit", "([BIJ)I");
    (*env)->DeleteLocalRef(env, listenerClass);
    if (g_on_video_setup == NULL || g_on_stage == NULL || g_on_stage_failed == NULL ||
        g_on_connected == NULL || g_on_terminated == NULL || g_on_decode_unit == NULL) {
        bridge_clear_exception(env);
        pthread_mutex_lock(&g_session_lock);
        g_started = 0;
        (*env)->DeleteGlobalRef(env, g_listener);
        g_listener = NULL;
        pthread_mutex_unlock(&g_session_lock);
        return BRIDGE_ERR_STATE;
    }

    SERVER_INFORMATION serverInfo;
    LiInitializeServerInformation(&serverInfo);
    jstring addressStr = bridge_optional_string(env, address);
    jstring appVersionStr = bridge_optional_string(env, appVersion);
    jstring gfeVersionStr = bridge_optional_string(env, gfeVersion);
    jstring rtspUrlStr = bridge_optional_string(env, rtspSessionUrl);
    serverInfo.address = (*env)->GetStringUTFChars(env, addressStr, NULL);
    serverInfo.serverInfoAppVersion = (*env)->GetStringUTFChars(env, appVersionStr, NULL);
    serverInfo.serverInfoGfeVersion = (*env)->GetStringUTFChars(env, gfeVersionStr, NULL);
    serverInfo.rtspSessionUrl = (*env)->GetStringUTFChars(env, rtspUrlStr, NULL);
    serverInfo.serverCodecModeSupport = serverCodecModeSupport;

    STREAM_CONFIGURATION streamConfig;
    LiInitializeStreamConfiguration(&streamConfig);
    streamConfig.width = width;
    streamConfig.height = height;
    streamConfig.fps = fps;
    streamConfig.bitrate = bitrate;
    streamConfig.packetSize = packetSize;
    streamConfig.audioConfiguration = audioConfiguration;
    streamConfig.supportedVideoFormats = supportedVideoFormats;
    streamConfig.clientRefreshRateX100 = clientRefreshRateX100;
    streamConfig.streamingRemotely = streamingRemotely;
    if (remoteInputAesKey != NULL && remoteInputAesIv != NULL) {
        bridge_copy_bytes(env, remoteInputAesKey, streamConfig.remoteInputAesKey, sizeof(streamConfig.remoteInputAesKey));
        bridge_copy_bytes(env, remoteInputAesIv, streamConfig.remoteInputAesIv, sizeof(streamConfig.remoteInputAesIv));
        streamConfig.encryptionFlags = ENCFLG_ALL;
    } else {
        streamConfig.encryptionFlags = ENCFLG_NONE;
    }

    CONNECTION_LISTENER_CALLBACKS listenerCallbacks;
    LiInitializeConnectionCallbacks(&listenerCallbacks);
    listenerCallbacks.stageStarting = bridge_stage_starting;
    listenerCallbacks.stageFailed = bridge_stage_failed;
    listenerCallbacks.connectionStarted = bridge_connection_started;
    listenerCallbacks.connectionTerminated = bridge_connection_terminated;

    DECODER_RENDERER_CALLBACKS decoderCallbacks;
    LiInitializeVideoCallbacks(&decoderCallbacks);
    decoderCallbacks.setup = bridge_video_setup;
    decoderCallbacks.start = bridge_video_start;
    decoderCallbacks.stop = bridge_video_stop;
    decoderCallbacks.cleanup = bridge_video_cleanup;
    decoderCallbacks.submitDecodeUnit = bridge_submit_decode_unit;
    decoderCallbacks.capabilities = 0;

    AUDIO_RENDERER_CALLBACKS audioCallbacks;
    LiInitializeAudioCallbacks(&audioCallbacks);
    audioCallbacks.init = bridge_audio_init;
    audioCallbacks.start = bridge_audio_start;
    audioCallbacks.stop = bridge_audio_stop;
    audioCallbacks.cleanup = bridge_audio_cleanup;
    audioCallbacks.decodeAndPlaySample = bridge_audio_decode;
    audioCallbacks.capabilities = CAPABILITY_SUPPORTS_ARBITRARY_AUDIO_DURATION;

    BRIDGE_LOG("LiStartConnection %s\n", serverInfo.address);
    int result = LiStartConnection(&serverInfo, &streamConfig, &listenerCallbacks,
                                   &decoderCallbacks, &audioCallbacks, NULL, 0, NULL, 0);

    (*env)->ReleaseStringUTFChars(env, addressStr, serverInfo.address);
    (*env)->ReleaseStringUTFChars(env, appVersionStr, serverInfo.serverInfoAppVersion);
    (*env)->ReleaseStringUTFChars(env, gfeVersionStr, serverInfo.serverInfoGfeVersion);
    (*env)->ReleaseStringUTFChars(env, rtspUrlStr, serverInfo.rtspSessionUrl);
    if (addressStr != address) {
        (*env)->DeleteLocalRef(env, addressStr);
    }
    if (appVersionStr != appVersion) {
        (*env)->DeleteLocalRef(env, appVersionStr);
    }
    if (gfeVersionStr != gfeVersion) {
        (*env)->DeleteLocalRef(env, gfeVersionStr);
    }
    if (rtspUrlStr != rtspSessionUrl) {
        (*env)->DeleteLocalRef(env, rtspUrlStr);
    }

    pthread_mutex_lock(&g_session_lock);
    g_started = 0;
    g_stop_requested = 0;
    if (g_listener != NULL) {
        (*env)->DeleteGlobalRef(env, g_listener);
        g_listener = NULL;
    }
    pthread_mutex_unlock(&g_session_lock);
    BRIDGE_LOG("LiStartConnection returned %d\n", result);
    return result;
}

JNIEXPORT void JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeStopConnection(JNIEnv *env, jobject thiz) {
    (void)thiz;
    /* Only valid after the start thread has returned; the Kotlin wrapper joins first. */
    pthread_mutex_lock(&g_session_lock);
    int was_started = g_started;
    pthread_mutex_unlock(&g_session_lock);
    if (was_started) {
        return;
    }
    if (g_listener != NULL) {
        (*env)->DeleteGlobalRef(env, g_listener);
        g_listener = NULL;
    }
}

JNIEXPORT void JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeInterruptConnection(JNIEnv *env, jobject thiz) {
    (void)env;
    (void)thiz;
    LiInterruptConnection();
}

JNIEXPORT jboolean JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeIsActive(JNIEnv *env, jobject thiz) {
    (void)env;
    (void)thiz;
    pthread_mutex_lock(&g_session_lock);
    jboolean active = (g_started && !g_stop_requested) ? JNI_TRUE : JNI_FALSE;
    pthread_mutex_unlock(&g_session_lock);
    return active;
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendKeyboardEvent(
        JNIEnv *env, jobject thiz, jshort keyCode, jbyte keyAction, jbyte modifiers, jbyte flags) {
    (void)env;
    (void)thiz;
    return LiSendKeyboardEvent2((short)keyCode, (char)keyAction, (char)modifiers, (char)flags);
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendUtf8TextEvent(
        JNIEnv *env, jobject thiz, jstring text) {
    (void)thiz;
    const char *utf8 = (*env)->GetStringUTFChars(env, text, NULL);
    if (utf8 == NULL) {
        bridge_clear_exception(env);
        return -1;
    }
    jsize length = (*env)->GetStringUTFLength(env, text);
    int result = LiSendUtf8TextEvent(utf8, (unsigned int)length);
    (*env)->ReleaseStringUTFChars(env, text, utf8);
    return result;
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendMouseMove(
        JNIEnv *env, jobject thiz, jshort deltaX, jshort deltaY) {
    (void)env;
    (void)thiz;
    return LiSendMouseMoveEvent((short)deltaX, (short)deltaY);
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendMousePosition(
        JNIEnv *env, jobject thiz, jshort x, jshort y, jshort referenceWidth, jshort referenceHeight) {
    (void)env;
    (void)thiz;
    return LiSendMousePositionEvent((short)x, (short)y, (short)referenceWidth, (short)referenceHeight);
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendMouseButton(
        JNIEnv *env, jobject thiz, jbyte buttonAction, jint button) {
    (void)env;
    (void)thiz;
    return LiSendMouseButtonEvent((char)buttonAction, (int)button);
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendScroll(
        JNIEnv *env, jobject thiz, jbyte scrollClicks) {
    (void)env;
    (void)thiz;
    return LiSendScrollEvent((signed char)scrollClicks);
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendHighResScroll(
        JNIEnv *env, jobject thiz, jshort scrollAmount) {
    (void)env;
    (void)thiz;
    return LiSendHighResScrollEvent((short)scrollAmount);
}
