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
print(f"patched SPN captain startup ordering at {spn_dir}")
