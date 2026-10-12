// Package declare reads the package declarations of spec/packages.md,
// packages/<name>/hive-package.json for the seven owners and
// contract/hive-package.json for the shared library, and checks that they
// agree with the specification: each follows its schema, every command has
// one owner, the declared call graph has no cycle, no two packages claim a
// part of a home or a name, and the naming rules of spec/naming.md hold. It
// also holds an executable's description to its package's declaration: what
// a command declares agrees with the package that owns it.
//
// The declarations are the registry. This package reads them and decides
// nothing of its own: a rule it checks is one a specification file states.
package declare
