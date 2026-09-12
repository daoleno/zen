/*
 * Zen remote-desktop JNI bridge for the pinned moonlight-common-c core.
 *
 * The pinned core (src/Connection.c) returns from LiStartConnection() once the
 * stream is established: on success it has invoked connectionStarted() and the
 * stream threads keep running; on failure it calls LiStopConnection() itself.
 * This bridge therefore owns one live session across setup, streaming, stop and
 * callback-thread retirement:
 *
 *  - LiStartConnection runs on the caller's start thread. A successful return
 *    keeps the Java listener referenced; callbacks continue to arrive.
 *  - LiStopConnection is the only full teardown and is called exactly once per
 *    session, from a thread that is not a core callback thread.
 *  - LiInterruptConnection only aborts a pending start; stop() uses it while a
 *    start is in flight and then waits for the start call to return. A stop
 *    requested from a core callback is deferred: that callback is running on
 *    the LiStartConnection thread (starting) or a stream thread (active), so
 *    waiting there would wait for itself.
 *  - Sessions are generation-bound. Deferred teardown from session A can never
 *    stop session B, and a retired session's global ref is cleared before the
 *    bridge publishes IDLE, so a new start cannot install a ref that the old
 *    cleanup then deletes.
 *  - Only the instance that started the session may stop it; input events and
 *    teardown calls from another instance are rejected.
 *
 * The bridge is protocol-free: pairing, RTSP, ENet, crypto, FEC and retransmit
 * remain upstream. It does not log key material, text events or frame content.
 */
#include <jni.h>
#include <pthread.h>
#include <stdint.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "Limelight.h"

#ifdef ZEN_BRIDGE_TEST
#include <unistd.h>
#endif

#ifdef __ANDROID__
#include <android/log.h>
#define BRIDGE_LOG(...) __android_log_print(ANDROID_LOG_INFO, "zen-moonlight", __VA_ARGS__)
#else
#define BRIDGE_LOG(...) fprintf(stderr, "[zen-moonlight] " __VA_ARGS__)
#endif

#define BRIDGE_ERR_BUSY -2
#define BRIDGE_ERR_STATE -3
#define BRIDGE_ERR_KEY -4
#define BRIDGE_ERR_ARGUMENT -5

#define BRIDGE_KEY_BYTES 16
#define BRIDGE_MAX_TEXT_BYTES 4096
#define BRIDGE_MAX_DECODE_UNIT_BYTES (16 * 1024 * 1024)

#define SESSION_IDLE 0
#define SESSION_STARTING 1
#define SESSION_ACTIVE 2
#define SESSION_STOPPING 3

static JavaVM *g_vm;
static pthread_key_t g_env_key;
static pthread_once_t g_env_key_once = PTHREAD_ONCE_INIT;

static pthread_mutex_t g_lock = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t g_state_cond = PTHREAD_COND_INITIALIZER;
static pthread_mutex_t g_cb_lock = PTHREAD_MUTEX_INITIALIZER;
static pthread_cond_t g_cb_cond = PTHREAD_COND_INITIALIZER;

static int g_state = SESSION_IDLE;
static int g_stop_requested;
static int g_callback_failed;
static int g_terminated;
static int g_last_error;
static int g_last_start_error;
static int g_callbacks_inflight;
static uint64_t g_generation;
static jobject g_session_ref;

static jmethodID g_on_video_setup;
static jmethodID g_on_stage;
static jmethodID g_on_stage_complete;
static jmethodID g_on_stage_failed;
static jmethodID g_on_connected;
static jmethodID g_on_start_failed;
static jmethodID g_on_terminated;
static jmethodID g_on_status;
static jmethodID g_on_decode;

/* Set while a bridge callback runs on this thread; a callback must never wait
 * for the start/stream thread that is executing it. */
static __thread int t_callback_depth;

#ifdef ZEN_BRIDGE_TEST
static int g_test_teardown_delay_ms;
#endif

/* ---- JNI environment ------------------------------------------------ */

static void bridge_thread_detach(void *value) {
    (void)value;
    if (g_vm != NULL) {
        (*g_vm)->DetachCurrentThread(g_vm);
    }
}

static void bridge_make_env_key(void) {
    pthread_key_create(&g_env_key, bridge_thread_detach);
}

static JNIEnv *bridge_env(void) {
    JNIEnv *env = NULL;
    if (g_vm == NULL) {
        return NULL;
    }
    if ((*g_vm)->GetEnv(g_vm, (void **)&env, JNI_VERSION_1_6) == JNI_OK) {
        return env;
    }
    pthread_once(&g_env_key_once, bridge_make_env_key);
    env = (JNIEnv *)pthread_getspecific(g_env_key);
    if (env != NULL) {
        return env;
    }
#ifdef __ANDROID__
    if ((*g_vm)->AttachCurrentThread(g_vm, &env, NULL) != JNI_OK) {
        return NULL;
    }
#else
    if ((*g_vm)->AttachCurrentThread(g_vm, (void **)&env, NULL) != JNI_OK) {
        return NULL;
    }
#endif
    pthread_setspecific(g_env_key, env);
    return env;
}

static void bridge_clear_exception(JNIEnv *env) {
    if ((*env)->ExceptionCheck(env)) {
        (*env)->ExceptionDescribe(env);
        (*env)->ExceptionClear(env);
    }
}

static void bridge_request_teardown(uint64_t generation);

/* Returns 1 when the Java callback threw and the session is being retired. */
static int bridge_handle_callback_exception(JNIEnv *env) {
    int state;
    uint64_t generation;
    if (!(*env)->ExceptionCheck(env)) {
        return 0;
    }
    bridge_clear_exception(env);
    pthread_mutex_lock(&g_lock);
    g_callback_failed = 1;
    state = g_state;
    generation = g_generation;
    if (state == SESSION_STARTING) {
        /* Abort the in-flight start instead of waiting for it. */
        g_stop_requested = 1;
    }
    pthread_mutex_unlock(&g_lock);
    if (state == SESSION_STARTING) {
        LiInterruptConnection();
    }
    bridge_request_teardown(generation);
    return 1;
}

/* ---- session reference / callback lifetime -------------------------- */

static jobject bridge_enter_callback(void) {
    jobject listener;
    pthread_mutex_lock(&g_cb_lock);
    g_callbacks_inflight++;
    pthread_mutex_unlock(&g_cb_lock);
    pthread_mutex_lock(&g_lock);
    listener = g_session_ref;
    pthread_mutex_unlock(&g_lock);
    t_callback_depth++;
    return listener;
}

static void bridge_leave_callback(void) {
    t_callback_depth--;
    pthread_mutex_lock(&g_cb_lock);
    g_callbacks_inflight--;
    if (g_callbacks_inflight == 0) {
        pthread_cond_broadcast(&g_cb_cond);
    }
    pthread_mutex_unlock(&g_cb_lock);
}

/* Drops the generation's listener once no bridge callback can be running.
 * Called after the core joined its stream threads and before publishing IDLE. */
static void bridge_release_session_ref(uint64_t generation) {
    JNIEnv *env = bridge_env();
    jobject ref = NULL;
    pthread_mutex_lock(&g_cb_lock);
    while (g_callbacks_inflight > 0) {
        pthread_cond_wait(&g_cb_cond, &g_cb_lock);
    }
    pthread_mutex_lock(&g_lock);
    if (g_generation == generation && g_session_ref != NULL) {
        ref = g_session_ref;
        g_session_ref = NULL;
    }
    pthread_mutex_unlock(&g_lock);
    pthread_mutex_unlock(&g_cb_lock);
    if (ref != NULL && env != NULL) {
        (*env)->DeleteGlobalRef(env, ref);
    }
}

static int bridge_owns(JNIEnv *env, jobject thiz) {
    int owner;
    pthread_mutex_lock(&g_lock);
    owner = g_session_ref != NULL && (*env)->IsSameObject(env, thiz, g_session_ref);
    pthread_mutex_unlock(&g_lock);
    return owner;
}

/* ---- teardown ------------------------------------------------------- */

/* Publishes IDLE only after the generation's callbacks and ref are retired. */
static void bridge_finish_stop(uint64_t generation) {
    bridge_release_session_ref(generation);
    pthread_mutex_lock(&g_lock);
    if (g_generation == generation) {
        g_state = SESSION_IDLE;
        g_stop_requested = 0;
        g_terminated = 0;
        pthread_cond_broadcast(&g_state_cond);
    }
    pthread_mutex_unlock(&g_lock);
}

/* Runs LiStopConnection exactly once for the generation this call owns.
 * A stale generation is a no-op; a session still STARTING is only aborted. */
static int bridge_stop_owned_session(uint64_t generation) {
    int owned = 0;
    int starting = 0;
    pthread_mutex_lock(&g_lock);
    if (g_generation != generation) {
        pthread_mutex_unlock(&g_lock);
        return 0;
    }
    if (g_state == SESSION_ACTIVE) {
        g_state = SESSION_STOPPING;
        owned = 1;
    } else if (g_state == SESSION_STARTING) {
        g_stop_requested = 1;
        starting = 1;
    }
    pthread_mutex_unlock(&g_lock);
    if (starting) {
        LiInterruptConnection();
        return 0;
    }
    if (!owned) {
        return 0;
    }
    BRIDGE_LOG("LiStopConnection\n");
    LiStopConnection();
    bridge_finish_stop(generation);
    return 1;
}

typedef struct {
    uint64_t generation;
} bridge_teardown_request;

static void *bridge_teardown_thread(void *context) {
    bridge_teardown_request *request = (bridge_teardown_request *)context;
    uint64_t generation = request->generation;
    free(request);
#ifdef ZEN_BRIDGE_TEST
    if (g_test_teardown_delay_ms > 0) {
        usleep((useconds_t)g_test_teardown_delay_ms * 1000);
    }
#endif
    bridge_stop_owned_session(generation);
    return NULL;
}

static void bridge_request_teardown(uint64_t generation) {
    bridge_teardown_request *request = (bridge_teardown_request *)malloc(sizeof(*request));
    pthread_t thread;
    if (request == NULL) {
        return;
    }
    request->generation = generation;
    if (pthread_create(&thread, NULL, bridge_teardown_thread, request) == 0) {
        pthread_detach(thread);
    } else {
        free(request);
        BRIDGE_LOG("teardown thread creation failed\n");
    }
}

/* Called from a core callback thread: defer instead of waiting for self. */
static int bridge_defer_stop(JNIEnv *env, jobject thiz) {
    int state;
    int owner;
    uint64_t generation;
    pthread_mutex_lock(&g_lock);
    state = g_state;
    generation = g_generation;
    owner = g_session_ref != NULL && (*env)->IsSameObject(env, thiz, g_session_ref);
    if (owner && state == SESSION_STARTING) {
        g_stop_requested = 1;
    }
    pthread_mutex_unlock(&g_lock);
    if (!owner) {
        return state == SESSION_IDLE ? 0 : BRIDGE_ERR_STATE;
    }
    if (state == SESSION_STARTING) {
        LiInterruptConnection();
    } else if (state == SESSION_ACTIVE) {
        bridge_request_teardown(generation);
    }
    return 0;
}

/* ---- decoder renderer callbacks ------------------------------------- */

static int bridge_video_setup(int videoFormat, int width, int height, int redrawRate, void *context, int drFlags) {
    (void)context;
    (void)drFlags;
    jobject listener = bridge_enter_callback();
    JNIEnv *env = listener != NULL ? bridge_env() : NULL;
    int result = -1;
    if (env != NULL && listener != NULL) {
        jint accepted = (*env)->CallIntMethod(env, listener, g_on_video_setup,
                                              (jint)videoFormat, (jint)width, (jint)height, (jint)redrawRate);
        if (!bridge_handle_callback_exception(env)) {
            result = accepted == 0 ? 0 : -1;
        }
    }
    bridge_leave_callback();
    return result;
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
    jobject listener = bridge_enter_callback();
    JNIEnv *env = listener != NULL ? bridge_env() : NULL;
    int result = DR_NEED_IDR;

    if (decodeUnit == NULL || decodeUnit->fullLength <= 0 ||
        decodeUnit->fullLength > BRIDGE_MAX_DECODE_UNIT_BYTES) {
        bridge_leave_callback();
        return DR_NEED_IDR;
    }

    int declared = 0;
    for (PLENTRY entry = decodeUnit->bufferList; entry != NULL; entry = entry->next) {
        if (entry->length <= 0 || entry->data == NULL ||
            declared > BRIDGE_MAX_DECODE_UNIT_BYTES - entry->length) {
            declared = -1;
            break;
        }
        declared += entry->length;
    }
    if (declared <= 0 || declared > decodeUnit->fullLength) {
        bridge_leave_callback();
        return DR_NEED_IDR;
    }

    if (env != NULL && listener != NULL) {
        jbyteArray frame = (*env)->NewByteArray(env, (jsize)decodeUnit->fullLength);
        if (frame != NULL) {
            jsize offset = 0;
            int valid = 1;
            for (PLENTRY entry = decodeUnit->bufferList; entry != NULL; entry = entry->next) {
                if (offset + entry->length > decodeUnit->fullLength) {
                    valid = 0;
                    break;
                }
                (*env)->SetByteArrayRegion(env, frame, offset, (jsize)entry->length,
                                           (const jbyte *)entry->data);
                if ((*env)->ExceptionCheck(env)) {
                    valid = 0;
                    break;
                }
                offset += (jsize)entry->length;
            }
            if (valid) {
                jint accepted = (*env)->CallIntMethod(env, listener, g_on_decode, frame,
                                                      (jint)decodeUnit->frameType,
                                                      (jlong)decodeUnit->presentationTimeUs);
                if (!bridge_handle_callback_exception(env) && accepted == DR_OK) {
                    result = DR_OK;
                }
            } else {
                bridge_handle_callback_exception(env);
                result = DR_NEED_IDR;
            }
            (*env)->DeleteLocalRef(env, frame);
        } else {
            bridge_handle_callback_exception(env);
        }
    }
    bridge_leave_callback();
    return result;
}

/* ---- connection listener callbacks ---------------------------------- */

static void bridge_stage_starting(int stage) {
    jobject listener = bridge_enter_callback();
    JNIEnv *env = listener != NULL ? bridge_env() : NULL;
    if (env != NULL && listener != NULL) {
        (*env)->CallVoidMethod(env, listener, g_on_stage, (jint)stage);
        bridge_handle_callback_exception(env);
    }
    bridge_leave_callback();
}

static void bridge_stage_complete(int stage) {
    jobject listener = bridge_enter_callback();
    JNIEnv *env = listener != NULL ? bridge_env() : NULL;
    if (env != NULL && listener != NULL) {
        (*env)->CallVoidMethod(env, listener, g_on_stage_complete, (jint)stage);
        bridge_handle_callback_exception(env);
    }
    bridge_leave_callback();
}

static void bridge_stage_failed(int stage, int errorCode) {
    jobject listener = bridge_enter_callback();
    JNIEnv *env = listener != NULL ? bridge_env() : NULL;
    if (env != NULL && listener != NULL) {
        (*env)->CallVoidMethod(env, listener, g_on_stage_failed, (jint)stage, (jint)errorCode);
        bridge_handle_callback_exception(env);
    }
    bridge_leave_callback();
}

static void bridge_connection_started(void) {
    jobject listener = bridge_enter_callback();
    JNIEnv *env = listener != NULL ? bridge_env() : NULL;
    if (env != NULL && listener != NULL) {
        (*env)->CallVoidMethod(env, listener, g_on_connected);
        bridge_handle_callback_exception(env);
    }
    bridge_leave_callback();
}

static void bridge_connection_status(int status) {
    jobject listener = bridge_enter_callback();
    JNIEnv *env = listener != NULL ? bridge_env() : NULL;
    if (env != NULL && listener != NULL) {
        (*env)->CallVoidMethod(env, listener, g_on_status, (jint)status);
        bridge_handle_callback_exception(env);
    }
    bridge_leave_callback();
}

static void bridge_connection_terminated(int errorCode) {
    jobject listener = bridge_enter_callback();
    JNIEnv *env = listener != NULL ? bridge_env() : NULL;
    int stale;
    uint64_t generation;
    pthread_mutex_lock(&g_lock);
    stale = (g_state == SESSION_IDLE) || g_terminated;
    generation = g_generation;
    g_terminated = 1;
    g_last_error = errorCode;
    pthread_mutex_unlock(&g_lock);
    if (!stale && env != NULL && listener != NULL) {
        (*env)->CallVoidMethod(env, listener, g_on_terminated, (jint)errorCode);
        bridge_handle_callback_exception(env);
    }
    bridge_leave_callback();
    if (!stale) {
        /* Upstream may leave stream threads alive after termination. */
        bridge_request_teardown(generation);
    }
}

/* Surfaces a failed start to the caller before the listener is retired. */
static void bridge_notify_start_failed(uint64_t generation, int errorCode) {
    jobject listener = bridge_enter_callback();
    JNIEnv *env = listener != NULL ? bridge_env() : NULL;
    int current;
    pthread_mutex_lock(&g_lock);
    current = g_generation == generation;
    pthread_mutex_unlock(&g_lock);
    if (env != NULL && listener != NULL && current) {
        (*env)->CallVoidMethod(env, listener, g_on_start_failed, (jint)errorCode);
        bridge_handle_callback_exception(env);
    }
    bridge_leave_callback();
}

/* ---- audio renderer callbacks (no audio pipeline in the prototype) -- */

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

/* ---- validation helpers --------------------------------------------- */

static int bridge_copy_key_material(JNIEnv *env, jbyteArray key, jbyteArray iv,
                                    char *keyOut, char *ivOut) {
    int keyNonZero = 0;
    int i;
    if (key == NULL || iv == NULL) {
        return 0;
    }
    if ((*env)->GetArrayLength(env, key) != BRIDGE_KEY_BYTES ||
        (*env)->GetArrayLength(env, iv) != BRIDGE_KEY_BYTES) {
        return 0;
    }
    (*env)->GetByteArrayRegion(env, key, 0, BRIDGE_KEY_BYTES, (jbyte *)keyOut);
    (*env)->GetByteArrayRegion(env, iv, 0, BRIDGE_KEY_BYTES, (jbyte *)ivOut);
    if ((*env)->ExceptionCheck(env)) {
        bridge_clear_exception(env);
        return 0;
    }
    for (i = 0; i < BRIDGE_KEY_BYTES; i++) {
        if (keyOut[i] != 0) {
            keyNonZero = 1;
            break;
        }
    }
    return keyNonZero;
}

/* Same packed layout as upstream MAKE_AUDIO_CONFIGURATION: magic 0xCA, channel
 * count in bits 8..15, channel mask in bits 16..31. */
static int bridge_validate_audio_configuration(int audioConfiguration) {
    int channelCount = (audioConfiguration >> 8) & 0xFF;
    return (audioConfiguration & 0xFF) == 0xCA && channelCount >= 1 && channelCount <= 8;
}

static int bridge_validate_stream_arguments(int width, int height, int fps, int bitrate, int packetSize) {
    return width >= 16 && width <= 8192 &&
           height >= 16 && height <= 8192 &&
           fps >= 1 && fps <= 240 &&
           bitrate >= 100 && bitrate <= 200000 &&
           packetSize >= 64 && packetSize <= 65536;
}

/* ---- JNI entry points ----------------------------------------------- */

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
    char keyMaterial[BRIDGE_KEY_BYTES];
    char ivMaterial[BRIDGE_KEY_BYTES];
    uint64_t generation;
    int result;

    if (!bridge_validate_stream_arguments(width, height, fps, bitrate, packetSize) ||
        !bridge_validate_audio_configuration(audioConfiguration) ||
        supportedVideoFormats == 0 || address == NULL || serverCodecModeSupport == 0) {
        return BRIDGE_ERR_ARGUMENT;
    }
    if (!bridge_copy_key_material(env, remoteInputAesKey, remoteInputAesIv, keyMaterial, ivMaterial)) {
        BRIDGE_LOG("refusing start without valid 16-byte input key/IV\n");
        return BRIDGE_ERR_KEY;
    }

    pthread_mutex_lock(&g_lock);
    if (g_state != SESSION_IDLE) {
        pthread_mutex_unlock(&g_lock);
        return BRIDGE_ERR_BUSY;
    }
    generation = ++g_generation;
    g_state = SESSION_STARTING;
    g_stop_requested = 0;
    g_callback_failed = 0;
    g_terminated = 0;
    g_last_error = 0;
    g_last_start_error = 0;
    g_session_ref = (*env)->NewGlobalRef(env, thiz);
    if (g_session_ref == NULL) {
        g_state = SESSION_IDLE;
        pthread_mutex_unlock(&g_lock);
        return BRIDGE_ERR_STATE;
    }
    pthread_mutex_unlock(&g_lock);

    jclass listenerClass = (*env)->GetObjectClass(env, thiz);
    g_on_video_setup = (*env)->GetMethodID(env, listenerClass, "onVideoSetup", "(IIII)I");
    g_on_stage = (*env)->GetMethodID(env, listenerClass, "onConnectionStage", "(I)V");
    g_on_stage_complete = (*env)->GetMethodID(env, listenerClass, "onConnectionStageComplete", "(I)V");
    g_on_stage_failed = (*env)->GetMethodID(env, listenerClass, "onConnectionStageFailed", "(II)V");
    g_on_connected = (*env)->GetMethodID(env, listenerClass, "onConnectionStarted", "()V");
    g_on_start_failed = (*env)->GetMethodID(env, listenerClass, "onConnectionStartFailed", "(I)V");
    g_on_status = (*env)->GetMethodID(env, listenerClass, "onConnectionStatusUpdate", "(I)V");
    g_on_terminated = (*env)->GetMethodID(env, listenerClass, "onConnectionTerminated", "(I)V");
    g_on_decode = (*env)->GetMethodID(env, listenerClass, "onDecodeUnit", "([BIJ)I");
    (*env)->DeleteLocalRef(env, listenerClass);
    if (g_on_video_setup == NULL || g_on_stage == NULL || g_on_stage_complete == NULL ||
        g_on_stage_failed == NULL || g_on_connected == NULL || g_on_start_failed == NULL ||
        g_on_status == NULL || g_on_terminated == NULL || g_on_decode == NULL) {
        bridge_clear_exception(env);
        bridge_release_session_ref(generation);
        pthread_mutex_lock(&g_lock);
        if (g_generation == generation) {
            g_state = SESSION_IDLE;
            pthread_cond_broadcast(&g_state_cond);
        }
        pthread_mutex_unlock(&g_lock);
        return BRIDGE_ERR_STATE;
    }

    SERVER_INFORMATION serverInfo;
    LiInitializeServerInformation(&serverInfo);
    jstring addressStr = address;
    jstring appVersionStr = appVersion != NULL ? appVersion : (*env)->NewStringUTF(env, "");
    jstring gfeVersionStr = gfeVersion != NULL ? gfeVersion : (*env)->NewStringUTF(env, "");
    jstring rtspUrlStr = rtspSessionUrl != NULL ? rtspSessionUrl : (*env)->NewStringUTF(env, "");
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
    /* Validated client-generated material only; no unencrypted fallback. */
    memcpy(streamConfig.remoteInputAesKey, keyMaterial, BRIDGE_KEY_BYTES);
    memcpy(streamConfig.remoteInputAesIv, ivMaterial, BRIDGE_KEY_BYTES);
    streamConfig.encryptionFlags = ENCFLG_ALL;

    CONNECTION_LISTENER_CALLBACKS listenerCallbacks;
    LiInitializeConnectionCallbacks(&listenerCallbacks);
    listenerCallbacks.stageStarting = bridge_stage_starting;
    listenerCallbacks.stageComplete = bridge_stage_complete;
    listenerCallbacks.stageFailed = bridge_stage_failed;
    listenerCallbacks.connectionStarted = bridge_connection_started;
    listenerCallbacks.connectionStatusUpdate = bridge_connection_status;
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

    BRIDGE_LOG("LiStartConnection\n");
    result = LiStartConnection(&serverInfo, &streamConfig, &listenerCallbacks,
                               &decoderCallbacks, &audioCallbacks, NULL, 0, NULL, 0);

    (*env)->ReleaseStringUTFChars(env, addressStr, serverInfo.address);
    (*env)->ReleaseStringUTFChars(env, appVersionStr, serverInfo.serverInfoAppVersion);
    (*env)->ReleaseStringUTFChars(env, gfeVersionStr, serverInfo.serverInfoGfeVersion);
    (*env)->ReleaseStringUTFChars(env, rtspUrlStr, serverInfo.rtspSessionUrl);
    if (appVersionStr != appVersion && appVersion == NULL) {
        (*env)->DeleteLocalRef(env, appVersionStr);
    }
    if (gfeVersionStr != gfeVersion && gfeVersion == NULL) {
        (*env)->DeleteLocalRef(env, gfeVersionStr);
    }
    if (rtspUrlStr != rtspSessionUrl && rtspSessionUrl == NULL) {
        (*env)->DeleteLocalRef(env, rtspUrlStr);
    }

    if (result == 0) {
        int stopWanted;
        pthread_mutex_lock(&g_lock);
        stopWanted = (g_generation != generation) ||
                     g_stop_requested || g_callback_failed || g_terminated;
        if (g_generation == generation) {
            g_state = SESSION_ACTIVE;
            pthread_cond_broadcast(&g_state_cond);
        }
        pthread_mutex_unlock(&g_lock);
        BRIDGE_LOG("stream active\n");
        if (stopWanted) {
            bridge_stop_owned_session(generation);
        }
        return 0;
    }

    /* The core undid its own partial work; surface the failure, then retire. */
    {
        int stopRequested;
        int callbackFailed;
        pthread_mutex_lock(&g_lock);
        stopRequested = g_stop_requested;
        callbackFailed = g_callback_failed;
        if (g_generation == generation) {
            g_last_start_error = result;
        }
        pthread_mutex_unlock(&g_lock);
        if (!stopRequested || callbackFailed) {
            bridge_notify_start_failed(generation, result);
        }
    }
    BRIDGE_LOG("LiStartConnection failed: %d\n", result);
    bridge_release_session_ref(generation);
    pthread_mutex_lock(&g_lock);
    if (g_generation == generation) {
        g_state = SESSION_IDLE;
        g_stop_requested = 0;
        pthread_cond_broadcast(&g_state_cond);
    }
    pthread_mutex_unlock(&g_lock);
    return result;
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeStopConnection(JNIEnv *env, jobject thiz) {
    uint64_t generation;

    /* A callback runs on the thread that would have to finish starting or
     * streaming, so it must never wait for that thread. */
    if (t_callback_depth > 0) {
        return bridge_defer_stop(env, thiz);
    }

    for (;;) {
        pthread_mutex_lock(&g_lock);
        if (g_state == SESSION_IDLE) {
            pthread_mutex_unlock(&g_lock);
            return 0;
        }
        if (g_session_ref == NULL || !(*env)->IsSameObject(env, thiz, g_session_ref)) {
            pthread_mutex_unlock(&g_lock);
            return BRIDGE_ERR_STATE;
        }
        generation = g_generation;
        if (g_state == SESSION_STARTING) {
            g_stop_requested = 1;
            pthread_mutex_unlock(&g_lock);
            LiInterruptConnection();
            pthread_mutex_lock(&g_lock);
            while (g_state == SESSION_STARTING && g_generation == generation) {
                pthread_cond_wait(&g_state_cond, &g_lock);
            }
            int generationChanged = g_generation != generation;
            pthread_mutex_unlock(&g_lock);
            if (generationChanged) {
                return 0;
            }
            continue;
        }
        if (g_state == SESSION_STOPPING) {
            while (g_state == SESSION_STOPPING) {
                pthread_cond_wait(&g_state_cond, &g_lock);
            }
            pthread_mutex_unlock(&g_lock);
            return 0;
        }
        pthread_mutex_unlock(&g_lock);
        bridge_stop_owned_session(generation);
        return 0;
    }
}

JNIEXPORT void JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeInterruptConnection(JNIEnv *env, jobject thiz) {
    int starting;
    pthread_mutex_lock(&g_lock);
    starting = g_state == SESSION_STARTING && g_session_ref != NULL &&
               (*env)->IsSameObject(env, thiz, g_session_ref);
    pthread_mutex_unlock(&g_lock);
    if (starting) {
        LiInterruptConnection();
    }
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSessionState(JNIEnv *env, jobject thiz) {
    int state;
    int flags = 0;
    (void)env;
    (void)thiz;
    pthread_mutex_lock(&g_lock);
    state = g_state;
    if (g_terminated) {
        flags |= 0x10;
    }
    if (g_callback_failed) {
        flags |= 0x20;
    }
    pthread_mutex_unlock(&g_lock);
    return state | flags;
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeLastError(JNIEnv *env, jobject thiz) {
    int value;
    (void)env;
    (void)thiz;
    pthread_mutex_lock(&g_lock);
    value = g_last_error;
    pthread_mutex_unlock(&g_lock);
    return value;
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeLastStartError(JNIEnv *env, jobject thiz) {
    int value;
    (void)env;
    (void)thiz;
    pthread_mutex_lock(&g_lock);
    value = g_last_start_error;
    pthread_mutex_unlock(&g_lock);
    return value;
}

static int bridge_snapshot_state_is_active(void) {
    int active;
    pthread_mutex_lock(&g_lock);
    active = g_state == SESSION_ACTIVE;
    pthread_mutex_unlock(&g_lock);
    return active;
}

JNIEXPORT jboolean JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeIsActive(JNIEnv *env, jobject thiz) {
    (void)env;
    (void)thiz;
    return bridge_snapshot_state_is_active();
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendKeyboardEvent(
        JNIEnv *env, jobject thiz, jshort keyCode, jbyte keyAction, jbyte modifiers, jbyte flags) {
    if (!bridge_owns(env, thiz) || !bridge_snapshot_state_is_active()) {
        return BRIDGE_ERR_STATE;
    }
    return LiSendKeyboardEvent2((short)keyCode, (char)keyAction, (char)modifiers, (char)flags);
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendUtf8TextEvent(
        JNIEnv *env, jobject thiz, jbyteArray utf8) {
    int result;
    jsize length;
    char *buffer;

    if (!bridge_owns(env, thiz) || !bridge_snapshot_state_is_active()) {
        return BRIDGE_ERR_STATE;
    }
    if (utf8 == NULL) {
        return BRIDGE_ERR_ARGUMENT;
    }
    length = (*env)->GetArrayLength(env, utf8);
    if (length <= 0 || length > BRIDGE_MAX_TEXT_BYTES) {
        return BRIDGE_ERR_ARGUMENT;
    }
    buffer = (char *)malloc((size_t)length);
    if (buffer == NULL) {
        return -1;
    }
    (*env)->GetByteArrayRegion(env, utf8, 0, length, (jbyte *)buffer);
    if ((*env)->ExceptionCheck(env)) {
        bridge_clear_exception(env);
        free(buffer);
        return -1;
    }
    /* Kotlin hands over standard UTF-8 bytes; JNI modified UTF-8 is never used. */
    result = LiSendUtf8TextEvent(buffer, (unsigned int)length);
    free(buffer);
    return result;
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendMouseMove(
        JNIEnv *env, jobject thiz, jshort deltaX, jshort deltaY) {
    if (!bridge_owns(env, thiz) || !bridge_snapshot_state_is_active()) {
        return BRIDGE_ERR_STATE;
    }
    return LiSendMouseMoveEvent((short)deltaX, (short)deltaY);
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendMousePosition(
        JNIEnv *env, jobject thiz, jshort x, jshort y, jshort referenceWidth, jshort referenceHeight) {
    if (!bridge_owns(env, thiz) || !bridge_snapshot_state_is_active()) {
        return BRIDGE_ERR_STATE;
    }
    return LiSendMousePositionEvent((short)x, (short)y, (short)referenceWidth, (short)referenceHeight);
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendMouseButton(
        JNIEnv *env, jobject thiz, jbyte buttonAction, jint button) {
    if (!bridge_owns(env, thiz) || !bridge_snapshot_state_is_active()) {
        return BRIDGE_ERR_STATE;
    }
    return LiSendMouseButtonEvent((char)buttonAction, (int)button);
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendScroll(
        JNIEnv *env, jobject thiz, jbyte scrollClicks) {
    if (!bridge_owns(env, thiz) || !bridge_snapshot_state_is_active()) {
        return BRIDGE_ERR_STATE;
    }
    return LiSendScrollEvent((signed char)scrollClicks);
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightCore_nativeSendHighResScroll(
        JNIEnv *env, jobject thiz, jshort scrollAmount) {
    if (!bridge_owns(env, thiz) || !bridge_snapshot_state_is_active()) {
        return BRIDGE_ERR_STATE;
    }
    return LiSendHighResScrollEvent((short)scrollAmount);
}

#ifdef ZEN_BRIDGE_TEST
/* Test-only hook: deterministically delay a detached teardown so a stale
 * generation can be observed racing a newly started session. */
JNIEXPORT void JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_setTeardownDelayMs(
        JNIEnv *env, jobject thiz, jint milliseconds) {
    (void)env;
    (void)thiz;
    g_test_teardown_delay_ms = milliseconds > 0 ? (int)milliseconds : 0;
}
#endif
