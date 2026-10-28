package cli

import "runtime/debug"

// versionString собирает строку для futi --version. Приоритет: версия из
// ldflags, затем версия модуля из build info: тег при go install @tag,
// псевдоверсия из VCS при локальной сборке (Go 1.24+). Ветка (devel) с
// коротким хэшем и +dirty — для сборок без псевдоверсии.
func versionString(ldVersion string, info *debug.BuildInfo, ok bool) string {
	if ldVersion != "" {
		return "futi " + ldVersion
	}
	if !ok || info == nil {
		return "futi (unknown)"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return "futi " + v
	}
	var rev string
	var dirty bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if rev == "" {
		return "futi (devel)"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "+dirty"
	}
	return "futi (devel) " + rev
}
