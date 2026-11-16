// Package settings serves the settings endpoints (ported from the Java SettingController,
// SettingService and SettingKeys, ADR-094 / doc 13 §7). It owns the `settings` table's WRITE path
// (single-key update); single-key READS from other domains (macro's derisk gate, the schedulers'
// config, purchase planning's account caps) are read-only (doc 13 §6.3).
//
// It also delivers doc 13 §7's design-improvement item: SettingKeys' stringly-typed key sprawl (raw
// string keys, type and default scattered across call sites) is replaced by the small typed
// registry in registry.go — name, type, and default declared once, compile-time checked.
package settings
