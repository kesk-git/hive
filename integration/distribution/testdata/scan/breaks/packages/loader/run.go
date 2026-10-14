package loader

func lookup(dlsym func(uintptr, string) uintptr, lib uintptr) uintptr {
	return dlsym(lib, "posix_spawn")
}

func lookupWord(h uintptr) uintptr { return Dlsym(h, "system") }
