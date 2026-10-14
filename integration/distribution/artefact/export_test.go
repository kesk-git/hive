package artefact

// SetHardLinkForTest replaces the function that makes a hard link, and returns
// a function that restores it.
func SetHardLinkForTest(f func(oldname, newname string) error) (restore func()) {
	old := hardLink
	hardLink = f
	return func() { hardLink = old }
}
