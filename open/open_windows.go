// +build windows

package open

func open(what string) error   { return Run("cmd", "/c", "start", "", what) }
func openURL(url string) error { return open(url) }
