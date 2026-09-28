// Minimal native wrapper for the usage widget served by Garcon.
import AppKit
import WebKit

let widgetURL = URL(string: "http://127.0.0.1:4141/usage-widget/")!

final class Widget: NSObject, NSApplicationDelegate, WKNavigationDelegate, WKUIDelegate {
    private let url: URL
    private var window: NSWindow!
    private var webView: WKWebView!

    init(url: URL) {
        self.url = url
        super.init()
    }

    func applicationDidFinishLaunching(_ notification: Notification) {
        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 380, height: 520),
            styleMask: [.titled, .closable, .miniaturizable, .resizable],
            backing: .buffered, defer: false
        )
        window.title = "Garcon Usage"
        window.isReleasedWhenClosed = false
        window.minSize = NSSize(width: 240, height: 200)
        window.level = .normal
        window.isMovable = true
        window.collectionBehavior = .managed
        window.center()

        webView = WKWebView(frame: .zero)
        webView.navigationDelegate = self
        webView.uiDelegate = self
        webView.allowsBackForwardNavigationGestures = true
        window.contentView = webView

        // Native menu shortcuts also make copy/paste work inside the web view.
        let menu = NSMenu()
        for (title, items) in [
            ("Garcon Usage", [("Quit", "terminate:", "q")]),
            ("Edit", [("Undo", "undo:", "z"), ("Cut", "cut:", "x"),
                      ("Copy", "copy:", "c"), ("Paste", "paste:", "v"),
                      ("Select All", "selectAll:", "a")]),
            ("Window", [("Close", "performClose:", "w"), ("Reload", "reload:", "r")])
        ] {
            let item = NSMenuItem()
            let submenu = NSMenu(title: title)
            for (label, action, key) in items {
                let command = NSMenuItem(title: label, action: Selector(action), keyEquivalent: key)
                if action == "reload:" { command.target = self }
                submenu.addItem(command)
            }
            item.submenu = submenu
            menu.addItem(item)
        }
        NSApp.mainMenu = menu
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
        reload(nil)
    }

    @objc private func reload(_ sender: Any?) {
        webView.load(URLRequest(url: url))
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool { true }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        window.title = webView.title ?? "Garcon Usage"
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        showError(error)
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        showError(error)
    }

    private func showError(_ error: Error) {
        guard (error as NSError).code != NSURLErrorCancelled else { return }
        let alert = NSAlert()
        alert.messageText = "Couldn’t load Garcon"
        alert.informativeText = "Make sure Garcon is running on port 4141, then press ⌘R to retry.\n\n" + error.localizedDescription
        alert.beginSheetModal(for: window)
    }

    // Keep links that request a new window inside this widget.
    func webView(_ webView: WKWebView, createWebViewWith configuration: WKWebViewConfiguration,
                 for navigationAction: WKNavigationAction, windowFeatures: WKWindowFeatures) -> WKWebView? {
        if navigationAction.targetFrame == nil { webView.load(navigationAction.request) }
        return nil
    }
}

let app = NSApplication.shared
let widget = Widget(url: widgetURL)
app.setActivationPolicy(.regular)
app.delegate = widget
app.run()
