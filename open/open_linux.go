// +build !windows,!darwin

package open

func open(what string) error   { return Run("xdg-open", what) }
func openURL(url string) error { return open(url) }
