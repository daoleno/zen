#ifndef ZEN_DESKTOP_PORTAL_H
#define ZEN_DESKTOP_PORTAL_H

#include <gio/gio.h>

typedef struct {
  GDBusConnection *bus;
  GCancellable *cancel;
  char *session;
  guint closed_subscription;
  guint32 node;
  int width, height, fd;
  gboolean control, revoked;
  GHashTable *keys;
  gboolean buttons[4];
} ZenPortal;

/* The caller supplies its explicit session bus; this module never opens a display. */
void zen_portal_init(ZenPortal *portal, GDBusConnection *bus, GCancellable *cancel);
gboolean zen_portal_open(ZenPortal *portal, gboolean control, const char *parent_window, GError **error);
gboolean zen_portal_input(ZenPortal *portal, const char *type, double x, double y,
                          guint32 code, gboolean down, int delta, GError **error);
void zen_portal_close(ZenPortal *portal);

/* Pure protocol boundaries are exposed for tests without compositor access. */
gboolean zen_portal_parse_grant(GVariant *results, gboolean control, guint32 *node,
                               int *width, int *height, GError **error);
GVariant *zen_portal_input_parameters(const char *session, guint32 node, int width, int height,
                                     const char *type, double x, double y, guint32 code,
                                     gboolean down, int delta, const char **method, GError **error);

#endif
