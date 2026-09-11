CC ?= cc
OUT ?= $(ZEN_BUILD_TMPDIR)/zen-desktop-agent

.PHONY: all
all:
	@test -n "$(ZEN_BUILD_TMPDIR)"
	$(CC) -std=c11 -O2 -Wall -Wextra -Werror host-agent.c -o "$(OUT)" $$(pkg-config --cflags --libs gstreamer-app-1.0 gio-unix-2.0 x11 xtst)
