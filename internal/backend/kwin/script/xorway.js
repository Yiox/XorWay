// XorWay — script KWin 6.
// Envoie la liste des fenêtres au daemon (org.xorway.Daemon) à chaque
// changement. Chargé et déchargé par le daemon via org.kde.KWin /Scripting.

const SERVICE = "org.xorway.Daemon";
const PATH = "/org/xorway/Daemon";
const IFACE = "org.xorway.Daemon";

function relevant(w) {
    return (w.normalWindow || w.dialog) && !w.skipTaskbar && !w.deleted;
}

// L'API ne documente aucune propriété X11/Wayland, mais la conversion d'un
// QObject en chaîne donne sa classe C++ : « KWin::X11Window(0x…) » pour un
// client XWayland, « KWin::XdgToplevelWindow(0x…) » pour un client Wayland.
function kind(w) {
    const m = String(w).match(/^([A-Za-z_][\w:]*)\(/);
    return m ? m[1] : "";
}

function describe(w) {
    return {
        id: String(w.internalId),
        title: w.caption,
        resourceClass: w.resourceClass,
        desktopFile: w.desktopFileName,
        pid: w.pid,
        kind: kind(w),
    };
}

// excluded : fenêtre en cours de suppression, encore présente dans la liste.
function send(excluded) {
    const list = workspace.windowList()
        .filter(w => w !== excluded && relevant(w))
        .map(describe);
    callDBus(SERVICE, PATH, IFACE, "Update", JSON.stringify(list));
}

function watch(w) {
    const resend = () => send();
    w.captionChanged.connect(resend);
    w.skipTaskbarChanged.connect(resend);
    w.windowClassChanged.connect(resend);
    w.desktopFileNameChanged.connect(resend);
}

workspace.windowList().forEach(watch);
workspace.windowAdded.connect(w => { watch(w); send(); });
workspace.windowRemoved.connect(w => send(w));
send();
