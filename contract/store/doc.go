// Package store is Hive's durable record layer: atomic replacement of one
// record file, intent and roll-forward for several records, transaction
// locks and the check of the home's layout (spec/records.md).
package store
