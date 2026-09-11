#ifndef ZEN_DESKTOP_ENCODER_H
#define ZEN_DESKTOP_ENCODER_H

#include <gio/gio.h>
#include <gst/gst.h>

typedef struct {
  gint width, height, fps;
  guint bitrate;
  const char *render_node;
  GCancellable *cancel;
} ZenEncoderConfig;

typedef struct {
  GstElement *bin;
  gboolean hardware;
  const char *name;
  const char *reason;
} ZenEncoderSelection;

typedef GstElement *(*ZenEncoderAttempt)(const ZenEncoderConfig *config,
  const char *factory, gboolean hardware, gpointer data, GError **error);

gboolean zen_encoder_config_valid(const ZenEncoderConfig *config);
gboolean zen_encoder_render_node_valid(const char *path);
gboolean zen_encoder_caps_supported(const ZenEncoderConfig *config,
  GstCaps *sink, GstCaps *src, gboolean hardware);
/* Consumes bin on both success and failure; no probe frames leave this pipeline. */
gboolean zen_encoder_probe(GstElement *bin, const ZenEncoderConfig *config, GError **error);
GstElement *zen_encoder_try_factory(const ZenEncoderConfig *config, const char *factory,
  gboolean hardware, gpointer unused, GError **error);
/* Selection over already-verified device facts; injectable attempts keep failure
   and ordering tests independent of installed GPUs. Production uses select(). */
gboolean zen_encoder_select_verified(const ZenEncoderConfig *config, gboolean device_valid,
  const char *const *factories, guint count, ZenEncoderAttempt attempt, gpointer data,
  ZenEncoderSelection *selection, GError **error);
gboolean zen_encoder_select(const ZenEncoderConfig *config, ZenEncoderSelection *selection, GError **error);

#endif
