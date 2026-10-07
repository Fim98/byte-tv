package app

// desktopBindings is set by the Wails binding-generator build (go build -tags bindings).
// That process must not open a window or touch the user's library.
var desktopBindings bool
