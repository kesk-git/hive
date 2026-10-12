package exec

// checkLimits refuses a command line over the platform's limit before
// anything starts: 32,767 UTF-16 code units for the whole command line after
// quoting.
func checkLimits(p plan) *StartError {
	return checkWindowsLimit(windowsCommandLine(p.path, p.args))
}
