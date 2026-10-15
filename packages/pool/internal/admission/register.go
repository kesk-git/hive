package admission

import "github.com/kesk-git/hive/packages/pool/internal/entry"

func init() { entry.RegisterAdmit(entry.Default, Create, Check) }
