#ifndef ZEN_DESKTOP_PORTAL_H
#define ZEN_DESKTOP_PORTAL_H

#include <gio/gio.h>

/* Result of one input attempt: the caller keeps the stream on rejection and
 * only retires the portal when the session itself is gone or unauthorized. */
typedef enum {
  ZEN_PORTAL_INPUT_TERMINAL = -1,
  ZEN_PORTAL_INPUT_REJECTED = 0,
  ZEN_PORTAL_INPUT_OK = 1
} ZenPortalInputResult;

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
  /* Advertised interface versions and the single-use restore token returned by
   * Start. The token is opaque; the helper owns its persistence. */
  guint32 remote_version, screen_version;
  char *restore_token;
} ZenPortal;

/* The caller supplies its explicit session bus; this module never opens a display. */
void zen_portal_init(ZenPortal *portal, GDBusConnection *bus, GCancellable *cancel);

/* restore_token may be NULL. On success the portal owns any newly returned token
 * and exposes it through zen_portal_restore_token() until zen_portal_close(). */
gboolean zen_portal_open(ZenPortal *portal, gboolean control, const char *parent_window,
                         const char *restore_token, GError **error);
ZenPortalInputResult zen_portal_input(ZenPortal *portal, const char *type, double x, double y,
                                      guint32 code, gboolean down, int delta, GError **error);
void zen_portal_close(ZenPortal *portal);

/* The most recent portal-issued restore token, or NULL. Owned by the portal. */
const char *zen_portal_restore_token(const ZenPortal *portal);

/* Pure protocol boundaries are exposed for tests without compositor access. */
gboolean zen_portal_parse_grant(GVariant *results, gboolean control, guint32 *node,
                               int *width, int *height, GError **error);
GVariant *zen_portal_input_parameters(const char *session, guint32 node, int width, int height,
                                     const char *type, double x, double y, guint32 code,
                                     gboolean down, int delta, const char **method, GError **error);

/* Reports a D-Bus error that means this session bus has no usable portal. */
gboolean zen_portal_error_is_unavailable(const GError *error);

/* Reports an error that ends the portal session (revoked, unauthorized, gone).
 * Rejected single inputs stay non-terminal so the video stream survives. */
gboolean zen_portal_error_is_terminal(const GError *error);

/* Restore-token persistence. The file is created 0600 by the owner only; a
 * symlink, foreign owner, group/world-readable file or oversized content is
 * treated as unavailable and never follows the link. */
char *zen_portal_restore_token_load(const char *path, GError **error);
gboolean zen_portal_restore_token_store(const char *path, const char *token, GError **error);
gboolean zen_portal_restore_token_clear(const char *path, GError **error);

#endif
