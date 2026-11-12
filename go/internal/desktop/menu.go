package desktop

// Menu actions sent to the page (the web app listens for these).
const (
	ActionNew       = "new"
	ActionPalette   = "palette"
	ActionImport    = "import"
	ActionSettings  = "settings"
	ActionDashboard = "dashboard"
	ActionLibrary   = "library"
	ActionHelp      = "help"
	// ActionOpenData is handled natively (opens the data folder).
	ActionOpenData = "open-data-dir"
	MenuEvent      = "lorekeeper:menu"
)

// MenuItem is one entry of the application menu, independent of the toolkit that draws it.
type MenuItem struct {
	Label       string
	Action      string
	Accelerator string // "CmdOrCtrl+N" style
	Role        string // "quit", "edit", "window", ...: a platform standard menu or item
	Separator   bool
	Submenu     []MenuItem
}

// BuildMenu returns the application menu for an OS (goos is runtime.GOOS), with the same items and
// shortcuts as the Electron build. The standard Edit, Window and app menus are roles drawn by the
// platform. Two small differences from Electron: on macOS "Settings…" lives in the File menu (the
// standard app menu cannot be extended), and there are no zoom items.
func BuildMenu(goos string) []MenuItem {
	mac := goos == "darwin"
	sep := MenuItem{Separator: true}
	var menu []MenuItem
	if mac {
		menu = append(menu, MenuItem{Label: "Lorekeeper", Role: "app"})
	}
	settings := MenuItem{Label: "Settings…", Action: ActionSettings, Accelerator: "Ctrl+,"}
	if mac {
		settings.Accelerator = "Cmd+,"
	}
	file := []MenuItem{
		{Label: "New Title…", Action: ActionNew, Accelerator: "CmdOrCtrl+N"},
		{Label: "Import / Export…", Action: ActionImport, Accelerator: "CmdOrCtrl+Shift+I"},
		sep,
		{Label: "Open Data Folder", Action: ActionOpenData},
		sep,
		settings,
	}
	if !mac {
		file = append(file, MenuItem{Label: "Quit", Role: "quit", Accelerator: "Ctrl+Q"})
	}
	menu = append(menu, MenuItem{Label: "File", Submenu: file})
	menu = append(menu, MenuItem{Label: "Edit", Role: "edit"})
	menu = append(menu, MenuItem{Label: "View", Submenu: []MenuItem{
		{Label: "Dashboard", Action: ActionDashboard, Accelerator: "CmdOrCtrl+1"},
		{Label: "Library", Action: ActionLibrary, Accelerator: "CmdOrCtrl+2"},
		{Label: "Command Palette", Action: ActionPalette, Accelerator: "CmdOrCtrl+K"},
		sep,
		{Label: "Toggle Full Screen", Role: "fullscreen"},
	}})
	if mac {
		menu = append(menu, MenuItem{Label: "Window", Role: "window"})
	}
	menu = append(menu, MenuItem{Label: "Help", Submenu: []MenuItem{
		{Label: "Keyboard Shortcuts", Action: ActionHelp},
		{Label: "Open Data Folder", Action: ActionOpenData},
	}})
	return menu
}
