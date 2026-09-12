/*
 * Inert moonlight-common-c boundary for host JVM bridge tests.
 *
 * It implements exactly the Li* surface the Zen bridge links against, records
 * calls, and lets the test drive lifecycle events (start result, blocked start,
 * termination, decode units) without any network, host or real stream. It is
 * built only into libzen_moonlight_test.so and never into the app.
 */
#include <jni.h>
#include <pthread.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>
#include <unistd.h>

#include "Limelight.h"

#define FAKE_MAX_TEXT 4096

static int g_start_count;
static int g_stop_count;
static int g_interrupt_count;
static int g_start_result;
static volatile int g_start_blocks;
static volatile int g_start_entered;
static volatile int g_interrupted;
static CONNECTION_LISTENER_CALLBACKS g_listener;
static DECODER_RENDERER_CALLBACKS g_video;
static AUDIO_RENDERER_CALLBACKS g_audio;
static char g_last_text[FAKE_MAX_TEXT];
static int g_last_text_len;
static short g_last_keycode;

void LiInitializeServerInformation(PSERVER_INFORMATION info) {
    memset(info, 0, sizeof(*info));
}

void LiInitializeStreamConfiguration(PSTREAM_CONFIGURATION config) {
    memset(config, 0, sizeof(*config));
}

void LiInitializeConnectionCallbacks(PCONNECTION_LISTENER_CALLBACKS callbacks) {
    memset(callbacks, 0, sizeof(*callbacks));
}

void LiInitializeVideoCallbacks(PDECODER_RENDERER_CALLBACKS callbacks) {
    memset(callbacks, 0, sizeof(*callbacks));
}

void LiInitializeAudioCallbacks(PAUDIO_RENDERER_CALLBACKS callbacks) {
    memset(callbacks, 0, sizeof(*callbacks));
}

int LiStartConnection(PSERVER_INFORMATION serverInfo, PSTREAM_CONFIGURATION streamConfig,
                      PCONNECTION_LISTENER_CALLBACKS clCallbacks, PDECODER_RENDERER_CALLBACKS drCallbacks,
                      PAUDIO_RENDERER_CALLBACKS arCallbacks, void *renderContext, int drFlags,
                      void *audioContext, int arFlags) {
    (void)serverInfo;
    (void)streamConfig;
    (void)renderContext;
    (void)drFlags;
    (void)audioContext;
    (void)arFlags;
    g_listener = *clCallbacks;
    g_video = *drCallbacks;
    g_audio = *arCallbacks;
    g_start_count++;

    if (g_start_blocks) {
        g_start_entered = 1;
        while (!g_interrupted) {
            usleep(2000);
        }
        return -1;
    }
    if (g_start_result != 0) {
        if (g_listener.stageFailed != NULL) {
            g_listener.stageFailed(STAGE_RTSP_HANDSHAKE, g_start_result);
        }
        return g_start_result;
    }
    if (g_listener.stageStarting != NULL) {
        g_listener.stageStarting(STAGE_PLATFORM_INIT);
    }
    if (g_video.setup != NULL && g_video.setup(VIDEO_FORMAT_H264, 1920, 1080, 60, NULL, 0) != 0) {
        if (g_listener.stageFailed != NULL) {
            g_listener.stageFailed(STAGE_VIDEO_STREAM_INIT, -1);
        }
        return -1;
    }
    if (g_listener.stageComplete != NULL) {
        g_listener.stageComplete(STAGE_PLATFORM_INIT);
    }
    if (g_listener.connectionStarted != NULL) {
        g_listener.connectionStarted();
    }
    return 0;
}

void LiStopConnection(void) {
    g_stop_count++;
}

void LiInterruptConnection(void) {
    g_interrupt_count++;
    g_interrupted = 1;
}

int LiSendKeyboardEvent2(short keyCode, char keyAction, char modifiers, char flags) {
    (void)keyAction;
    (void)modifiers;
    (void)flags;
    g_last_keycode = keyCode;
    return 0;
}

int LiSendUtf8TextEvent(const char *text, unsigned int length) {
    if (text == NULL || length > FAKE_MAX_TEXT) {
        return -1;
    }
    memcpy(g_last_text, text, length);
    g_last_text_len = (int)length;
    return 0;
}

int LiSendMouseMoveEvent(short deltaX, short deltaY) {
    (void)deltaX;
    (void)deltaY;
    return 0;
}

int LiSendMousePositionEvent(short x, short y, short referenceWidth, short referenceHeight) {
    (void)x;
    (void)y;
    (void)referenceWidth;
    (void)referenceHeight;
    return 0;
}

int LiSendMouseButtonEvent(char action, int button) {
    (void)action;
    (void)button;
    return 0;
}

int LiSendScrollEvent(signed char scrollClicks) {
    (void)scrollClicks;
    return 0;
}

int LiSendHighResScrollEvent(short scrollAmount) {
    (void)scrollAmount;
    return 0;
}

/* ---- JNI probe for MoonlightTestProbe ------------------------------- */

static void *fake_terminate_thread(void *context) {
    int errorCode = (int)(intptr_t)context;
    if (g_listener.connectionTerminated != NULL) {
        g_listener.connectionTerminated(errorCode);
    }
    return NULL;
}

JNIEXPORT void JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_reset(JNIEnv *env, jobject thiz) {
    (void)env;
    (void)thiz;
    g_start_count = 0;
    g_stop_count = 0;
    g_interrupt_count = 0;
    g_start_result = 0;
    g_start_blocks = 0;
    g_start_entered = 0;
    g_interrupted = 0;
    g_last_text_len = 0;
    g_last_keycode = 0;
    memset(&g_listener, 0, sizeof(g_listener));
    memset(&g_video, 0, sizeof(g_video));
    memset(&g_audio, 0, sizeof(g_audio));
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_startCount(JNIEnv *env, jobject thiz) {
    (void)env;
    (void)thiz;
    return g_start_count;
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_stopCount(JNIEnv *env, jobject thiz) {
    (void)env;
    (void)thiz;
    return g_stop_count;
}

JNIEXPORT jint JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_interruptCount(JNIEnv *env, jobject thiz) {
    (void)env;
    (void)thiz;
    return g_interrupt_count;
}

JNIEXPORT jboolean JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_startEntered(JNIEnv *env, jobject thiz) {
    (void)env;
    (void)thiz;
    return g_start_entered ? JNI_TRUE : JNI_FALSE;
}

JNIEXPORT void JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_setStartResult(JNIEnv *env, jobject thiz, jint result) {
    (void)env;
    (void)thiz;
    g_start_result = result;
}

JNIEXPORT void JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_setStartBlocks(JNIEnv *env, jobject thiz, jboolean blocks) {
    (void)env;
    (void)thiz;
    g_start_blocks = blocks ? 1 : 0;
}

JNIEXPORT void JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_emitTerminated(JNIEnv *env, jobject thiz, jint errorCode) {
    pthread_t thread;
    (void)env;
    (void)thiz;
    if (pthread_create(&thread, NULL, fake_terminate_thread, (void *)(intptr_t)errorCode) == 0) {
        pthread_detach(thread);
    }
}

JNIEXPORT void JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_emitDecodeUnit(
        JNIEnv *env, jobject thiz, jbyteArray payload, jint frameType, jlong presentationTimeUs, jint declaredLength) {
    jsize length;
    char *data;
    LENTRY entry;
    DECODE_UNIT unit;
    (void)thiz;

    if (g_video.submitDecodeUnit == NULL || payload == NULL) {
        return;
    }
    length = (*env)->GetArrayLength(env, payload);
    if (length <= 0) {
        return;
    }
    data = (char *)malloc((size_t)length);
    if (data == NULL) {
        return;
    }
    (*env)->GetByteArrayRegion(env, payload, 0, length, (jbyte *)data);
    memset(&entry, 0, sizeof(entry));
    entry.data = data;
    entry.length = (int)length;
    entry.bufferType = BUFFER_TYPE_PICDATA;
    memset(&unit, 0, sizeof(unit));
    unit.fullLength = declaredLength > 0 ? declaredLength : (int)length;
    unit.frameType = frameType;
    unit.presentationTimeUs = (uint64_t)presentationTimeUs;
    unit.bufferList = &entry;
    g_video.submitDecodeUnit(&unit);
    free(data);
}

JNIEXPORT jbyteArray JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_lastUtf8Bytes(JNIEnv *env, jobject thiz) {
    jbyteArray result;
    (void)thiz;
    result = (*env)->NewByteArray(env, g_last_text_len);
    if (result != NULL && g_last_text_len > 0) {
        (*env)->SetByteArrayRegion(env, result, 0, g_last_text_len, (const jbyte *)g_last_text);
    }
    return result;
}

JNIEXPORT jshort JNICALL Java_expo_modules_zenremotedesktop_MoonlightTestProbe_lastKeyCode(JNIEnv *env, jobject thiz) {
    (void)env;
    (void)thiz;
    return g_last_keycode;
}
