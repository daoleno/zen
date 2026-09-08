#include "portal_capture.h"
#include <fcntl.h>

gboolean zen_portal_bind_source(ZenPortal *portal, GstElement *source, GError **error) {
  if (!source || !portal->session || !portal->node || portal->fd < 0 ||
      portal->revoked || !portal->cancel || g_cancellable_is_cancelled(portal->cancel) ||
      fcntl(portal->fd, F_GETFD) < 0) {
    g_set_error_literal(error, G_IO_ERROR, G_IO_ERROR_PERMISSION_DENIED, "No active portal capture grant.");
    return FALSE;
  }
  const char *properties[] = {"fd", "path", "keepalive-time", "resend-last", "on-disconnect", "max-buffers"};
  for (guint i = 0; i < G_N_ELEMENTS(properties); i++) {
    if (!g_object_class_find_property(G_OBJECT_GET_CLASS(source), properties[i])) {
      g_set_error_literal(error, G_IO_ERROR, G_IO_ERROR_NOT_SUPPORTED, "Required PipeWire source policies are unavailable.");
      return FALSE;
    }
  }
  char *node = g_strdup_printf("%u", portal->node);
  /* PipeWire's GStreamer core duplicates this FD when connecting. Keep the
     portal owner alive until the pipeline has returned to NULL. */
  g_object_set(source, "fd", portal->fd, "path", node, "keepalive-time", 0,
    "resend-last", FALSE, "on-disconnect", 2, "max-buffers", 2, NULL);
  g_free(node);
  return TRUE;
}
