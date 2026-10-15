// Package rulewrite builds the rule writers of the Pool (spec/pool.md, Rules,
// Subjects and Generated knowledge): hive-rule record, amend and expire, and
// hive-memory subject. It imports memory for the transaction planner and never
// lives in rule, which memory imports for the resolver.
package rulewrite
