// SPDX-License-Identifier: AGPL-3.0-or-later

// XorWay — KWin 6 script.
// Sends the window list to the daemon (org.xorway.Daemon) on every change.
// Loaded and unloaded by the daemon through org.kde.KWin /Scripting.

const SERVICE = "org.xorway.Daemon";
const PATH = "/org/xorway/Daemon";
const IFACE = "org.xorway.Daemon";

function relevant(w) {
    return (w.normalWindow || w.dialog) && !w.skipTaskbar && !w.deleted;
}

// The API documents no X11/Wayland property, but converting a QObject to a
// string gives its C++ class: "KWin::X11Window(0x…)" for an XWayland client,
// "KWin::XdgToplevelWindow(0x…)" for a Wayland client.
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

// excluded: window being removed, still present in the list.
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
