package setup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"maps"
	"strings"

	"gitlab.com/scabb-island/omaslack/internal/doctor"
)

// manifest maps each file setup installed to the sha256 of the content it
// wrote, so setup can tell its own files from the user's.
type manifest map[string]string

// manifestFile is setup.json.
type manifestFile struct {
	Files manifest `json:"files"`
}

// run is one setup or removal: the system, the options and the manifest
// as it changes.
type run struct {
	sys    System
	l      Layout
	dryRun bool
	saved  manifest // as read
	m      manifest // as it will be saved
}

func start(sys System, l Layout, opts Options) (*run, *doctor.Result) {
	m, err := loadManifest(sys.FS, l.Manifest)
	if err != nil {
		r := fail("manifest", "cannot read "+l.Manifest+": "+err.Error(), "delete it to start over: rm "+l.Manifest)
		return nil, &r
	}
	return &run{sys: sys, l: l, dryRun: opts.DryRun, saved: maps.Clone(m), m: m}, nil
}

func loadManifest(fsys FS, path string) (manifest, error) {
	data, err := fsys.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return manifest{}, nil
	}
	if err != nil {
		return nil, err
	}
	var f manifestFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, err
	}
	if f.Files == nil {
		f.Files = manifest{}
	}
	return f.Files, nil
}

// save writes the manifest if it changed, and removes it once it lists
// nothing.
func (r *run) save() []doctor.Result {
	if r.dryRun || maps.Equal(r.saved, r.m) {
		return nil
	}
	err := r.writeManifest()
	if err != nil {
		return []doctor.Result{fail("manifest", "cannot write "+r.l.Manifest+": "+err.Error(), "")}
	}
	return nil
}

func (r *run) writeManifest() error {
	if len(r.m) == 0 {
		return r.sys.FS.Remove(r.l.Manifest)
	}
	data, err := json.MarshalIndent(manifestFile{Files: r.m}, "", "  ")
	if err != nil {
		return err
	}
	return r.sys.FS.WriteFile(r.l.Manifest, append(data, '\n'), 0o600)
}

func digest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// install puts item in place unless the user changed it, and says whether
// it wrote it (or, in a dry run, would).
func (r *run) install(item Item) (doctor.Result, bool) {
	src, err := r.sys.FS.ReadFile(item.Source)
	if err != nil {
		return fail(item.Name, "cannot read the packaged file: "+err.Error(),
			"reinstall the package ("+installPackage+") or pass --from"), false
	}
	want := digest(src)
	cur, err := r.sys.FS.ReadFile(item.Target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return r.write(item, src, "install", "installed")
	case err != nil:
		return fail(item.Name, err.Error(), ""), false
	case digest(cur) == want:
		r.m[item.Target] = want
		return ok(item.Name, "already in place: "+item.Target, ""), false
	case digest(cur) == r.m[item.Target]:
		return r.write(item, src, "update", "updated to the packaged version:")
	}
	return warn(item.Name, item.Target+" has changes of yours, so it was kept; a newer version is available",
		"compare them: diff -u "+item.Target+" "+item.Source), false
}

// write writes item's packaged content, or says it would in a dry run.
func (r *run) write(item Item, src []byte, would, did string) (doctor.Result, bool) {
	if r.dryRun {
		return ok(item.Name, "would "+would+" "+item.Target, ""), true
	}
	if err := r.sys.FS.WriteFile(item.Target, src, item.Mode); err != nil {
		return fail(item.Name, err.Error(), ""), false
	}
	r.m[item.Target] = digest(src)
	return ok(item.Name, did+" "+item.Target, ""), true
}

// remove removes item if setup installed it or it is the packaged file,
// and the user did not change it.
func (r *run) remove(item Item) doctor.Result {
	cur, err := r.sys.FS.ReadFile(item.Target)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		delete(r.m, item.Target)
		return ok(item.Name, "not installed", "")
	case err != nil:
		return fail(item.Name, err.Error(), "")
	case !r.unchanged(item, digest(cur)):
		return warn(item.Name, item.Target+" has changes of yours, so it was kept", "rm "+item.Target)
	case r.dryRun:
		return ok(item.Name, "would remove "+item.Target, "")
	}
	if err := r.sys.FS.Remove(item.Target); err != nil {
		return fail(item.Name, err.Error(), "")
	}
	delete(r.m, item.Target)
	return ok(item.Name, "removed "+item.Target, "")
}

// unchanged says whether a file with checksum sum is what setup installed
// or what the package ships.
func (r *run) unchanged(item Item, sum string) bool {
	if sum == r.m[item.Target] {
		return true
	}
	src, err := r.sys.FS.ReadFile(item.Source)
	return err == nil && digest(src) == sum
}

// ruleLoaded says whether hyprland.lua loads the window rule, and how to
// load it if not. setup never edits the Hyprland config.
func (r *run) ruleLoaded() doctor.Result {
	const name = "window rule loaded"
	if r.loadsRule() {
		return ok(name, "hyprland.lua loads it", "")
	}
	return warn(name, "Hyprland does not load it yet, and omaslack never edits your Hyprland config",
		"add this line to "+r.l.HyprConfig+", after require(\"default.hypr.omarchy\"): "+requireLine)
}

func (r *run) loadsRule() bool {
	cfg, err := r.sys.FS.ReadFile(r.l.HyprConfig)
	return err == nil && bytes.Contains(cfg, []byte(ruleModule))
}

// render has Omarchy render the template for the current theme when the
// template changed or was never rendered.
func (r *run) render(ctx context.Context, templateChanged bool) doctor.Result {
	const name = "theme rendered"
	if !templateChanged && r.exists(r.l.Rendered) {
		return ok(name, "already rendered: "+r.l.Rendered, "")
	}
	if r.dryRun {
		return ok(name, "would run "+refreshTheme, "")
	}
	if _, err := r.sys.Exec(ctx, refreshTheme); err != nil {
		return warn(name, refreshTheme+" failed: "+err.Error(), refreshTheme)
	}
	if !r.exists(r.l.Rendered) {
		return warn(name, r.l.Rendered+" was not rendered: is an Omarchy theme set?", "omarchy-theme-set <theme>")
	}
	return ok(name, "rendered "+r.l.Rendered, "")
}

func (r *run) exists(path string) bool {
	_, err := r.sys.FS.ReadFile(path)
	return err == nil
}

// enable enables and starts the user service, if the package installed it.
func (r *run) enable(ctx context.Context) doctor.Result {
	u, res, done := r.service(ctx)
	switch {
	case done:
		return res
	case u.Load != "loaded":
		return warn(serviceCheck, "omaslack.service is not installed", installPackage)
	case u.UnitFile == "enabled" && u.Active == "active":
		return ok(serviceCheck, "already enabled and running", "")
	}
	return r.systemctl(ctx, enableService, "enabled and started")
}

// disable stops the user service and keeps it from starting at login.
func (r *run) disable(ctx context.Context) doctor.Result {
	u, res, done := r.service(ctx)
	switch {
	case done:
		return res
	case u.Load != "loaded":
		return ok(serviceCheck, "not installed", "")
	case u.UnitFile != "enabled" && u.Active != "active":
		return ok(serviceCheck, "already disabled and stopped", "")
	}
	return r.systemctl(ctx, disableService, "disabled and stopped")
}

const serviceCheck = "service"

// service reads the unit's state; done means res is the result already.
func (r *run) service(ctx context.Context) (u doctor.Unit, res doctor.Result, done bool) {
	u, err := doctor.ReadService(ctx, r.sys.Exec)
	if err != nil {
		return u, warn(serviceCheck, "cannot ask systemd: "+err.Error(), ""), true
	}
	return u, doctor.Result{}, false
}

// systemctl runs cmd, a systemctl --user command line, or says it would.
func (r *run) systemctl(ctx context.Context, cmd, did string) doctor.Result {
	if r.dryRun {
		return ok(serviceCheck, "would run "+cmd, "")
	}
	args := strings.Fields(cmd)
	if _, err := r.sys.Exec(ctx, args[0], args[1:]...); err != nil {
		return fail(serviceCheck, cmd+": "+err.Error(), daemonLog)
	}
	return ok(serviceCheck, did, "")
}
