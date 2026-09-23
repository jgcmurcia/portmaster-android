#!/usr/bin/env python3
from pathlib import Path
import shutil
import subprocess

root = Path(__file__).resolve().parents[1]
compat = root / "compat" / "portmaster_updates_android_compat.go"
geoip_compat = root / "compat" / "portmaster_geoip_android_compat.go"
pm_dir = Path(subprocess.check_output(
    ["go", "list", "-m", "-f", "{{.Dir}}", "github.com/safing/portmaster"],
    cwd=root,
    text=True,
).strip())
updates = pm_dir / "updates"
main = updates / "main.go"

updates.chmod(updates.stat().st_mode | 0o200)
main.chmod(main.stat().st_mode | 0o200)
shutil.copyfile(compat, updates / "android_compat.go")

source = main.read_text()
startup_anchor = """	err = registry.LoadIndexes(module.Ctx)
	if err != nil {
		log.Warningf("updates: failed to load indexes: %s", err)
	}
"""
startup_replacement = startup_anchor + """
	// Android compatibility: the legacy intel index no longer contains GeoIP.
	// Import the current versions from intel.v3.json before selecting resources.
	if compatErr := injectAndroidGeoIPCompat(registry); compatErr != nil {
		log.Warningf("updates: failed to inject Android GeoIP compatibility resources: %s", compatErr)
	}
"""
if "injectAndroidGeoIPCompat(registry)" not in source:
    if startup_anchor not in source:
        raise SystemExit("could not locate updater startup anchor")
    source = source.replace(startup_anchor, startup_replacement, 1)

update_anchor = """	if err = registry.UpdateIndexes(ctx); err != nil {
		err = fmt.Errorf("failed to update indexes: %w", err)
		return
	}
"""
update_replacement = update_anchor + """
	// Refresh compatibility versions on every update so Android follows the
	// current GeoIP artifacts without relying on the removed legacy index keys.
	if compatErr := injectAndroidGeoIPCompat(registry); compatErr != nil {
		log.Warningf("updates: failed to refresh Android GeoIP compatibility resources: %s", compatErr)
	}
"""
if source.count("injectAndroidGeoIPCompat(registry)") < 2:
    if update_anchor not in source:
        raise SystemExit("could not locate updater refresh anchor")
    source = source.replace(update_anchor, update_replacement, 1)

main.write_text(source)

# Verify unpacked MMDB data against the SHA-256 from the current v3 index before
# maxminddb opens it. This closes the legacy updater's unsigned-Intel gap.
geoip_dir = pm_dir / "intel" / "geoip"
geoip_db = geoip_dir / "database.go"
geoip_dir.chmod(geoip_dir.stat().st_mode | 0o200)
geoip_db.chmod(geoip_db.stat().st_mode | 0o200)
shutil.copyfile(geoip_compat, geoip_dir / "android_compat.go")
geoip_source = geoip_db.read_text()
verify_anchor = """	unpacked, err := f.Unpack(".gz", updater.UnpackGZIP)
	if err != nil {
		return nil, "", fmt.Errorf("unpacking file: %w", err)
	}

	return f, unpacked, nil
"""
verify_replacement = """	unpacked, err := f.Unpack(".gz", updater.UnpackGZIP)
	if err != nil {
		return nil, "", fmt.Errorf("unpacking file: %w", err)
	}
	if err := verifyAndroidGeoIPHash(resource, unpacked); err != nil {
		return nil, "", fmt.Errorf("verifying file: %w", err)
	}

	return f, unpacked, nil
"""
if "verifyAndroidGeoIPHash(resource, unpacked)" not in geoip_source:
    if verify_anchor not in geoip_source:
        raise SystemExit("could not locate GeoIP unpack anchor")
    geoip_source = geoip_source.replace(verify_anchor, verify_replacement, 1)
geoip_db.write_text(geoip_source)

# Portbase 0.16.6 predates Go's final slices.SortFunc API. The old comparator
# returned bool; Go 1.26 requires a three-way int comparator and can infer the
# slice/element generic types from the arguments.
portbase_dir = Path(subprocess.check_output(
    ["go", "list", "-m", "-f", "{{.Dir}}", "github.com/safing/portbase"],
    cwd=root,
    text=True,
).strip())
portbase_updater = portbase_dir / "updater" / "updating.go"
portbase_updater.parent.chmod(portbase_updater.parent.stat().st_mode | 0o200)
portbase_updater.chmod(portbase_updater.stat().st_mode | 0o200)
portbase_source = portbase_updater.read_text()
old_sort = """	slices.SortFunc[*ResourceVersion](toUpdate, func(a, b *ResourceVersion) bool {
		return a.resource.Identifier < b.resource.Identifier
	})
	slices.SortFunc[*ResourceVersion](missingSigs, func(a, b *ResourceVersion) bool {
		return a.resource.Identifier < b.resource.Identifier
	})
"""
new_sort = """	slices.SortFunc(toUpdate, func(a, b *ResourceVersion) int {
		if a.resource.Identifier < b.resource.Identifier {
			return -1
		}
		if a.resource.Identifier > b.resource.Identifier {
			return 1
		}
		return 0
	})
	slices.SortFunc(missingSigs, func(a, b *ResourceVersion) int {
		if a.resource.Identifier < b.resource.Identifier {
			return -1
		}
		if a.resource.Identifier > b.resource.Identifier {
			return 1
		}
		return 0
	})
"""
if old_sort in portbase_source:
    portbase_source = portbase_source.replace(old_sort, new_sort, 1)
elif new_sort not in portbase_source:
    raise SystemExit("could not locate legacy Portbase slices.SortFunc comparators")
portbase_updater.write_text(portbase_source)

# Ensure the SPN captain cannot race the updater on a clean Android install.
spn_dir = Path(subprocess.check_output(
    ["go", "list", "-m", "-f", "{{.Dir}}", "github.com/safing/spn"],
    cwd=root,
    text=True,
).strip())
captain_module = spn_dir / "captain" / "module.go"
captain_module.parent.chmod(captain_module.parent.stat().st_mode | 0o200)
captain_module.chmod(captain_module.stat().st_mode | 0o200)
captain_source = captain_module.read_text()
captain_anchor = 'module = modules.Register("captain", prep, start, stop, "base", "terminal", "cabin", "docks", "crew", "navigator", "sluice", "patrol", "netenv")'
captain_replacement = 'module = modules.Register("captain", prep, start, stop, "base", "terminal", "cabin", "docks", "crew", "navigator", "sluice", "patrol", "netenv", "updates")'
if captain_anchor in captain_source:
    captain_source = captain_source.replace(captain_anchor, captain_replacement, 1)
elif captain_replacement not in captain_source:
    raise SystemExit("could not locate SPN captain dependency anchor")
captain_module.write_text(captain_source)

print(f"patched Portmaster updater at {pm_dir}")
print(f"patched Portmaster GeoIP verification at {geoip_dir}")
print(f"patched Portbase updater compatibility at {portbase_updater}")
print(f"patched SPN captain startup ordering at {spn_dir}")

# Android intercepts packets through VpnService/gVisor. The desktop network
# debug report pulls in compat, whose module requires the Linux/Windows
# interception driver and prevents the entire Android module graph starting.
network_api = pm_dir / "network" / "api.go"
network_api.parent.chmod(network_api.parent.stat().st_mode | 0o200)
network_api.chmod(network_api.stat().st_mode | 0o200)
network_source = network_api.read_text()
compat_import = '\t"github.com/safing/portmaster/compat"\n'
compat_call = '\tcompat.AddToDebugInfo(di)'
android_note = '\t// Android VPN diagnostics are provided by the Android engine.'
if compat_import in network_source and compat_call in network_source:
    network_source = network_source.replace(compat_import, "", 1)
    network_source = network_source.replace(compat_call, android_note, 1)
elif android_note not in network_source:
    raise SystemExit("could not locate desktop network diagnostics dependency")
network_api.write_text(network_source)
print("removed desktop-only compat dependency from Android network diagnostics")

# Cold-start race: status workers read the subsystem dependency slices while
# Start builds them. Use the manager lock already held by all those readers.
subsystem_registry = portbase_dir / "modules" / "subsystems" / "registry.go"
subsystem_registry.parent.chmod(subsystem_registry.parent.stat().st_mode | 0o200)
subsystem_registry.chmod(subsystem_registry.stat().st_mode | 0o200)
subsystem_source = subsystem_registry.read_text()
start_anchor = "func (mng *Manager) Start() error {\n\tmng.immutable.Set()"
start_locked = "func (mng *Manager) Start() error {\n\tmng.l.Lock()\n\tdefer mng.l.Unlock()\n\tmng.immutable.Set()"
if start_anchor in subsystem_source:
    subsystem_source = subsystem_source.replace(start_anchor, start_locked, 1)
elif start_locked not in subsystem_source:
    raise SystemExit("could not locate subsystem initialization lock anchor")
subsystem_registry.write_text(subsystem_source)
print("serialized subsystem initialization with status readers")

# A fast task can replace t.ctx before the queue waiter reads it. Capture the
# completion channel under the task lock before launching either goroutine.
tasks_file = portbase_dir / "modules" / "tasks.go"
tasks_file.parent.chmod(tasks_file.parent.stat().st_mode | 0o200)
tasks_file.chmod(tasks_file.stat().st_mode | 0o200)
tasks_source = tasks_file.read_text()
wait_anchor = "\tgo t.executeWithLocking()\n\tgo func() {\n\t\tselect {\n\t\tcase <-t.ctx.Done():"
wait_fixed = "\tt.lock.Lock()\n\texecutionDone := t.ctx.Done()\n\tt.lock.Unlock()\n\tgo t.executeWithLocking()\n\tgo func() {\n\t\tselect {\n\t\tcase <-executionDone:"
if wait_anchor in tasks_source:
    tasks_source = tasks_source.replace(wait_anchor, wait_fixed, 1)
elif wait_fixed not in tasks_source:
    raise SystemExit("could not locate task completion context race")
tasks_file.write_text(tasks_source)
print("captured per-execution task completion channel")

# time.Ticker contains runtime-owned timer state and must not be copied. Modern
# Go crashes when Stop/Reset is called on the legacy wrapper's copied ticker.
sleepy_file = portbase_dir / "modules" / "sleepyticker.go"
sleepy_file.chmod(sleepy_file.stat().st_mode | 0o200)
sleepy_source = sleepy_file.read_text()
if "ticker         time.Ticker" in sleepy_source:
    sleepy_source = sleepy_source.replace("ticker         time.Ticker", "ticker         *time.Ticker", 1)
    sleepy_source = sleepy_source.replace("*time.NewTicker(normalDuration)", "time.NewTicker(normalDuration)", 1)
elif "ticker         *time.Ticker" not in sleepy_source:
    raise SystemExit("could not locate copied SleepyTicker")
sleepy_file.write_text(sleepy_source)
print("kept runtime ticker by pointer for safe Stop/Reset")

# Network detection and Android may request the first update concurrently while
# the updater is still starting. Synchronize the shared pending-update flag.
update_source = main.read_text()
if "updateASAP          bool" in update_source:
    update_source = update_source.replace('"time"', '"time"\n\t"sync/atomic"', 1)
    update_source = update_source.replace("updateASAP          bool", "updateASAP          atomic.Bool", 1)
    update_source = update_source.replace("if updateASAP {", "if updateASAP.Load() {", 1)
    update_source = update_source.replace("updateASAP = true", "updateASAP.Store(true)", 1)
elif "updateASAP          atomic.Bool" not in update_source:
    raise SystemExit("could not locate updater startup flag")
main.write_text(update_source)
print("made pending startup update flag atomic")
