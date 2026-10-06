package duif

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Only the compiled form of the independently tested EN/NL adapter is exempt.
// No entire file, storage API, or global CSS declaration is exempted.
func inspectFrontendChunk(js string) (string, int) {
	count := 0
	keys := map[string]bool{}
	keyPattern := regexp.MustCompile(`([A-Za-z_$][A-Za-z0-9_$]*)=["']duif\.ui-language["']`)
	for _, m := range keyPattern.FindAllStringSubmatch(js, -1) {
		keys[m[1]] = true
	}
	for _, method := range []string{"getItem", "setItem"} {
		ending := `\(([A-Za-z_$][A-Za-z0-9_$]*)\)`
		middle := `\)==null\?void 0:`
		if method == "setItem" {
			ending = `\(([A-Za-z_$][A-Za-z0-9_$]*),[A-Za-z_$][A-Za-z0-9_$]*\)`
			middle = `\)==null\|\|`
		}
		compiled := regexp.MustCompile(`\(([A-Za-z_$][A-Za-z0-9_$]*)=globalThis\.localStorage` + middle + `([A-Za-z_$][A-Za-z0-9_$]*)\.` + method + ending)
		js = compiled.ReplaceAllStringFunc(js, func(call string) string {
			m := compiled.FindStringSubmatch(call)
			if m[1] != m[2] || !keys[m[3]] {
				return call
			}
			count++
			return "validatedLanguagePreference"
		})
		native := regexp.MustCompile(`globalThis\.localStorage\?\.` + method + ending)
		js = native.ReplaceAllStringFunc(js, func(call string) string {
			m := native.FindStringSubmatch(call)
			if !keys[m[1]] {
				return call
			}
			count++
			return "validatedLanguagePreference"
		})
	}
	scoped := regexp.MustCompile(`(?:^|[{}])\.duif-root\{[^}]*\}`)
	js = scoped.ReplaceAllStringFunc(js, func(rule string) string {
		return strings.ReplaceAll(rule, "--background:var(--duif-ground)", "--validated-module-background:var(--duif-ground)")
	})
	return js, count
}

func TestFrontendBundleInspection_RejectsNewStorageAndGlobalCSS(t *testing.T) {
	valid := `const key="duif.ui-language";(x=globalThis.localStorage)==null?void 0:x.getItem(key);(y=globalThis.localStorage)==null||y.setItem(key,n);@layer base{.duif-root{--background:var(--duif-ground);color:red}}`
	got, count := inspectFrontendChunk(valid)
	if count != 2 || strings.Contains(got, "localStorage") || strings.Contains(got, "--background:") {
		t.Fatalf("valid adapter/scoped alias: %s (%d)", got, count)
	}
	for _, bad := range []string{
		`const key="transcript";(x=globalThis.localStorage)==null||x.setItem(key,n)`,
		`const key="duif.ui-language";(x=globalThis.localStorage)==null||other.setItem(key,n)`,
		`localStorage.setItem("anything",value)`,
		`:root,.duif-root{--background:var(--duif-ground)}`,
		`:root{--background:red}.duif-root{--background:var(--duif-ground)}`,
	} {
		result, _ := inspectFrontendChunk(bad)
		if !strings.Contains(result, "localStorage") && !strings.Contains(result, "--background:") {
			t.Fatalf("unapproved call/rule hidden: %s", bad)
		}
	}
}

type frontendManifestEntry struct {
	File           string   `json:"file"`
	Imports        []string `json:"imports"`
	DynamicImports []string `json:"dynamicImports"`
	IsEntry        bool     `json:"isEntry"`
}

// Every generated JS asset is inspected. The original 540 KiB limit applies
// to the full initial import chain, not just an artificially small entry file.
func TestMeetingFrontend_BundleIsClean(t *testing.T) {
	const dist = "frontend/dist"
	if _, err := os.Stat(dist + "/bundle.js"); err != nil {
		t.Skipf("geen gebouwde bundle (%v); draai make bundle", err)
	}
	chunks := map[string]string{}
	err := filepath.WalkDir(dist, func(file string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(file) != ".js" {
			return nil
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dist, file)
		chunks[filepath.ToSlash(rel)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	manifest := map[string]frontendManifestEntry{}
	manifestRaw, manifestErr := os.ReadFile(dist + "/.vite/manifest.json")
	if manifestErr == nil {
		if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
			t.Fatal(err)
		}
	}
	initial := map[string]bool{}
	if len(chunks) == 1 {
		initial["bundle.js"] = true
	} else {
		if manifestErr != nil {
			t.Fatal("multiple JS chunks require Vite manifest to measure initial imports")
		}
		visited := map[string]bool{}
		var visit func(string)
		visit = func(key string) {
			if visited[key] {
				return
			}
			visited[key] = true
			item, ok := manifest[key]
			if !ok {
				t.Fatalf("manifest references missing %s", key)
			}
			if strings.HasSuffix(item.File, ".js") {
				initial[item.File] = true
			}
			for _, dep := range item.Imports {
				visit(dep)
			}
		}
		entries := 0
		for key, item := range manifest {
			if item.IsEntry {
				entries++
				visit(key)
			}
		}
		if entries != 1 {
			t.Fatalf("expected one module entry, got %d", entries)
		}
	}
	for file := range initial {
		if _, ok := chunks[file]; !ok {
			t.Fatalf("initial manifest asset is missing: %s", file)
		}
	}
	t.Chdir("..")
	assets := New().Assets()
	if assets == nil {
		t.Fatal("Assets() = nil")
	}
	// The meeting loads its 3D renderer only when it opens. Give that lazy
	// asset its own budget while retaining both existing workspace budgets.
	modelChunk := manifest["src/lib/media/model/pigeonStage.ts"].File
	if modelChunk != "" && initial[modelChunk] {
		t.Fatal("3D renderer must not enter the initial workspace import chain")
	}
	total, initialBytes, modelBytes, preferenceCalls := 0, 0, 0, 0
	for file, js := range chunks {
		if _, err := fs.Stat(assets, file); err != nil {
			t.Errorf("built chunk %s not served: %v", file, err)
		}
		inspected, calls := inspectFrontendChunk(js)
		preferenceCalls += calls
		for _, forbidden := range []string{
			"localStorage", "sessionStorage", "indexedDB", "openDatabase", "MediaRecorder",
			"document.cookie", "createObjectURL", "showSaveFilePicker", "FileSystemWritableFileStream",
			"data:audio", "eval(", "new Function", "/Users/", "/home/", "sourceMappingURL",
			"--background:", "--overlay:", "EditorView", "gitgraph", "markdown-it", "html,:host", "text-size-adjust",
		} {
			if strings.Contains(inspected, forbidden) {
				t.Errorf("%s contains %q", file, forbidden)
			}
		}
		// Three embeds multiline GLSL strings: their preprocessor newlines are
		// required even in minified JS. Its byte budget checks the built output.
		if lines := strings.Count(js, "\n"); file != modelChunk && lines > 100 {
			t.Errorf("%s is not minified: %d lines", file, lines)
		}
		relative := regexp.MustCompile(`(?:from\s*|import\s*\(?)["'](\.[^"']+\.js)["']`)
		for _, m := range relative.FindAllStringSubmatch(js, -1) {
			target := path.Clean(path.Join(path.Dir(file), m[1]))
			if strings.HasPrefix(target, "../") {
				t.Errorf("%s imports outside assets: %s", file, target)
				continue
			}
			if _, ok := chunks[target]; !ok {
				t.Errorf("%s imports missing chunk %s", file, target)
			}
		}
		total += len(js)
		if file == modelChunk {
			modelBytes += len(js)
		}
		if initial[file] {
			initialBytes += len(js)
		}
	}
	if preferenceCalls != 2 {
		t.Errorf("expected exactly two validated language calls across all chunks, got %d", preferenceCalls)
	}
	const initialBudget = 540 * 1024
	// Extra on-demand UI has a separate total cap. This does not relax startup
	// or exempt any delayed code from the privacy and served-asset checks.
	//
	// Dit plafond bewaakt vooral onverwachte sprongen: een barrel-import die
	// een halve componentbibliotheek meesleept, een vergeten scan, een
	// afhankelijkheid die per ongeluk in de werkruimte belandt. Het bewaakt
	// bewust niet de groei van @parai/ui zelf: wat die bibliotheek aan gedeelde
	// componenten toevoegt mag de duif meenemen. Daarom ligt het plafond
	// ongeveer 5 % boven de gemeten bundel. Groeit @parai/ui er later overheen,
	// meet dan opnieuw en zet het plafond weer zo'n 5 % hoger; is de sprong
	// groter dan @parai/ui verklaart, zoek dan eerst wat er meekwam. Opruimen gaat
	// voor verhogen: de CSS-scan in frontend/parai-ui-sources.ts haalde zo
	// ruim 41 KB weg. De lazy 3D-renderer heeft zijn eigen plafond.
	const totalBudget = 832 * 1024
	if initialBytes > initialBudget {
		t.Errorf("initial JS chain %d bytes exceeds %d", initialBytes, initialBudget)
	}
	if total-modelBytes > totalBudget {
		t.Errorf("workspace JS %d bytes exceeds %d", total-modelBytes, totalBudget)
	}
	const modelBudget = 700 * 1024
	if modelBytes > modelBudget {
		t.Errorf("lazy 3D renderer %d bytes exceeds %d", modelBytes, modelBudget)
	}
	t.Logf("JS assets=%d initial=%d workspace=%d lazy3D=%d total=%d bytes", len(chunks), initialBytes, total-modelBytes, modelBytes, total)
}
