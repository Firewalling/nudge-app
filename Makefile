SHELL := /bin/sh

NPM ?= npm
GO ?= go
BINDIR ?= $(HOME)/.local/bin
DATADIR ?= $(if $(XDG_DATA_HOME),$(XDG_DATA_HOME),$(HOME)/.local/share)

.PHONY: desktop install-user uninstall-user

desktop:
	cd frontend && $(NPM) ci && VITE_NUDGE_DESKTOP=1 $(NPM) run build -- --outDir ../desktop/dist --emptyOutDir
	mkdir -p dist
	cd desktop && $(GO) build -mod=readonly -trimpath -buildvcs=false -tags production -o ../dist/nudge-desktop .

install-user: desktop
	@case "$(BINDIR)" in /|"") printf '%s\n' 'BINDIR must be an absolute non-root path' >&2; exit 1;; /*) ;; *) printf '%s\n' 'BINDIR must be an absolute non-root path' >&2; exit 1;; esac
	@case "$(DATADIR)" in /|"") printf '%s\n' 'DATADIR must be an absolute non-root path' >&2; exit 1;; /*) ;; *) printf '%s\n' 'DATADIR must be an absolute non-root path' >&2; exit 1;; esac
	install -d "$(BINDIR)" "$(DATADIR)/applications" "$(DATADIR)/icons/hicolor/scalable/apps"
	install -m 755 dist/nudge-desktop "$(BINDIR)/nudge-desktop"
	desktop-file-install --dir="$(DATADIR)/applications" --set-key=Exec --set-value="\"$(BINDIR)/nudge-desktop\"" desktop/org.wails.nudge.desktop
	install -m 644 desktop/org.wails.nudge.svg "$(DATADIR)/icons/hicolor/scalable/apps/org.wails.nudge.svg"

uninstall-user:
	@case "$(BINDIR)" in /|"") printf '%s\n' 'BINDIR must be an absolute non-root path' >&2; exit 1;; /*) ;; *) printf '%s\n' 'BINDIR must be an absolute non-root path' >&2; exit 1;; esac
	@case "$(DATADIR)" in /|"") printf '%s\n' 'DATADIR must be an absolute non-root path' >&2; exit 1;; /*) ;; *) printf '%s\n' 'DATADIR must be an absolute non-root path' >&2; exit 1;; esac
	rm -f "$(BINDIR)/nudge-desktop" "$(DATADIR)/applications/org.wails.nudge.desktop" "$(DATADIR)/icons/hicolor/scalable/apps/org.wails.nudge.svg"
