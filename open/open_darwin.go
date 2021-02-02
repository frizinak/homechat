// +build darwin

package open

func open(what string) error   { return Run("open", what) }
func openURL(url string) error { return open(url) }
