package extension

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	internalextension "github.com/kubesphere/ksctl/internal/extension"
)

type rawResult []byte

func (r rawResult) RawJSON() []byte {
	return r
}

type failingWriter struct {
	err error
}

func (w failingWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func boolPointer(value bool) *bool {
	return &value
}

func TestOutputStructuredPreservesUnknownFields(t *testing.T) {
	raw := rawResult(`{"apiVersion":"example/v1","future":{"enabled":true},"items":[]}`)

	var jsonOutput bytes.Buffer
	if err := writeStructured(&jsonOutput, raw, outputJSON); err != nil {
		t.Fatalf("writeStructured(JSON) error = %v", err)
	}
	if got, want := jsonOutput.String(), string(raw)+"\n"; got != want {
		t.Fatalf("JSON output = %q, want %q", got, want)
	}

	var yamlOutput bytes.Buffer
	if err := writeStructured(&yamlOutput, raw, outputYAML); err != nil {
		t.Fatalf("writeStructured(YAML) error = %v", err)
	}
	for _, want := range []string{"future:", "enabled: true", "items: []"} {
		if !strings.Contains(yamlOutput.String(), want) {
			t.Fatalf("YAML output = %q, want %q", yamlOutput.String(), want)
		}
	}
	if !strings.HasSuffix(yamlOutput.String(), "\n") {
		t.Fatalf("YAML output lacks final newline: %q", yamlOutput.String())
	}
}

func TestOutputListHeadersAndValues(t *testing.T) {
	extension := internalextension.Extension{
		Metadata: internalextension.ObjectMeta{
			Name: "demo",
			Labels: map[string]string{
				"kubesphere.io/category": "observability",
			},
		},
		Spec: internalextension.ExtensionSpec{
			Provider: map[string]*internalextension.Provider{
				"en": {Name: "KubeSphere"},
			},
		},
		Status: internalextension.ExtensionStatus{
			RecommendedVersion: "1.3.0",
			Enabled:            boolPointer(true),
		},
	}
	plan := internalextension.InstallPlan{
		Metadata: internalextension.ObjectMeta{Name: "demo"},
		Spec: internalextension.InstallPlanSpec{
			Extension: internalextension.ExtensionRef{Name: "demo", Version: "1.2.0"},
			Enabled:   true,
		},
		Status: internalextension.InstallPlanStatus{
			InstallationStatus: internalextension.InstallationStatus{
				State:   "Installed",
				Version: "1.2.0",
			},
		},
	}
	result := internalextension.ListResult{Items: []internalextension.ListItem{{
		Extension: internalextension.Object[internalextension.Extension]{Value: extension},
		InstallPlan: &internalextension.Object[internalextension.InstallPlan]{
			Value: plan,
		},
	}}}

	var table bytes.Buffer
	if err := printList(&table, result, outputTable); err != nil {
		t.Fatalf("printList(table) error = %v", err)
	}
	if got, want := table.String(),
		"NAME  CATEGORY       RECOMMENDED  INSTALLED  TARGET  STATE\n"+
			"demo  observability  1.3.0        1.2.0      1.2.0   Installed\n"; got != want {
		t.Fatalf("table = %q, want %q", got, want)
	}

	var wide bytes.Buffer
	if err := printList(&wide, result, outputWide); err != nil {
		t.Fatalf("printList(wide) error = %v", err)
	}
	if got, want := wide.String(),
		"NAME  CATEGORY       RECOMMENDED  INSTALLED  TARGET  STATE      PROVIDER    ENABLED\n"+
			"demo  observability  1.3.0        1.2.0      1.2.0   Installed  KubeSphere  true\n"; got != want {
		t.Fatalf("wide = %q, want %q", got, want)
	}
}

func TestOutputDoesNotReportUnobservedTargetAsInstalled(t *testing.T) {
	extension := internalextension.Extension{
		Metadata: internalextension.ObjectMeta{Name: "demo"},
	}
	plan := internalextension.InstallPlan{
		Metadata: internalextension.ObjectMeta{Name: "demo"},
		Spec: internalextension.InstallPlanSpec{
			Extension: internalextension.ExtensionRef{
				Name:    "demo",
				Version: "2.0.0",
			},
			Enabled: true,
		},
		Status: internalextension.InstallPlanStatus{
			InstallationStatus: internalextension.InstallationStatus{
				State: "Preparing",
			},
		},
	}
	item := internalextension.ListItem{
		Extension: internalextension.Object[internalextension.Extension]{
			Value: extension,
		},
		InstallPlan: &internalextension.Object[internalextension.InstallPlan]{
			Value: plan,
		},
	}

	var listOutput bytes.Buffer
	if err := printList(
		&listOutput,
		internalextension.ListResult{
			Items: []internalextension.ListItem{item},
		},
		outputTable,
	); err != nil {
		t.Fatalf("printList() error = %v", err)
	}
	if !strings.Contains(
		listOutput.String(),
		"demo  <none>    <none>       <none>     2.0.0",
	) {
		t.Fatalf("list output = %q", listOutput.String())
	}

	var showOutput bytes.Buffer
	if err := printShow(
		&showOutput,
		internalextension.ShowResult{
			Extension:   item.Extension,
			InstallPlan: item.InstallPlan,
		},
	); err != nil {
		t.Fatalf("printShow() error = %v", err)
	}
	if !strings.Contains(
		showOutput.String(),
		"Installed Version    <none>\n",
	) || !strings.Contains(
		showOutput.String(),
		"Target Version       2.0.0\n",
	) {
		t.Fatalf("show output = %q", showOutput.String())
	}
}

func TestOutputPrefersSuccessfulInstallPlanObservation(t *testing.T) {
	extension := internalextension.Extension{
		Metadata: internalextension.ObjectMeta{Name: "demo"},
		Status: internalextension.ExtensionStatus{
			State:            "Installing",
			Enabled:          boolPointer(false),
			InstalledVersion: "1.0.0",
		},
	}
	plan := internalextension.InstallPlan{
		Metadata: internalextension.ObjectMeta{Name: "demo"},
		Spec: internalextension.InstallPlanSpec{
			Extension: internalextension.ExtensionRef{
				Name:    "demo",
				Version: "2.0.0",
			},
			Enabled: true,
		},
		Status: internalextension.InstallPlanStatus{
			InstallationStatus: internalextension.InstallationStatus{
				State:   "Upgraded",
				Version: "2.0.0",
			},
			Enabled: boolPointer(true),
		},
	}
	result := internalextension.ShowResult{
		Extension: internalextension.Object[internalextension.Extension]{
			Value: extension,
		},
		InstallPlan: &internalextension.Object[internalextension.InstallPlan]{
			Value: plan,
		},
	}

	var output bytes.Buffer
	if err := printShow(&output, result); err != nil {
		t.Fatalf("printShow() error = %v", err)
	}
	for _, want := range []string{
		"State                Upgraded\n",
		"Enabled              true\n",
		"Installed Version    2.0.0\n",
		"Target Version       2.0.0\n",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("show output = %q, want %q", output.String(), want)
		}
	}
}

func TestOutputShowFieldOrderAndMissingScalars(t *testing.T) {
	extension := internalextension.Extension{
		Metadata: internalextension.ObjectMeta{Name: "demo"},
		Spec: internalextension.ExtensionSpec{
			DisplayName: map[string]string{"zh": "演示", "en": "Demo"},
		},
		Status: internalextension.ExtensionStatus{
			Versions: []internalextension.ExtensionVersionInfo{
				{Version: "1.2.0"},
				{Version: "1.1.0"},
			},
		},
	}
	result := internalextension.ShowResult{
		Extension: internalextension.Object[internalextension.Extension]{
			Value: extension,
		},
	}
	var output bytes.Buffer
	if err := printShow(&output, result); err != nil {
		t.Fatalf("printShow() error = %v", err)
	}
	wantOrder := []string{
		"Name",
		"Display Name",
		"Description",
		"Category",
		"Provider",
		"State",
		"Enabled",
		"Installed Version",
		"Target Version",
		"Recommended Version",
		"Versions",
		"Conditions",
	}
	last := -1
	for _, field := range wantOrder {
		index := strings.Index(output.String(), field)
		if index <= last {
			t.Fatalf("field %q out of order in %q", field, output.String())
		}
		last = index
	}
	if !strings.Contains(output.String(), "Display Name         Demo\n") ||
		!strings.Contains(output.String(), "Installed Version    <none>\n") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestOutputSelectedVersionFieldOrder(t *testing.T) {
	version := internalextension.ExtensionVersion{
		Metadata: internalextension.ObjectMeta{
			Name: "demo-v1",
			Labels: map[string]string{
				"kubesphere.io/extension-ref": "demo",
			},
		},
		Spec: internalextension.ExtensionVersionSpec{
			Version:          "v1.0.0+build",
			Category:         "observability",
			InstallationMode: "HostOnly",
			Namespace:        "demo-system",
			KSVersion:        ">=4",
			KubeVersion:      ">=1.27",
			ChartURL:         "oci://example/demo",
			ExternalDependencies: []internalextension.ExternalDependency{{
				Name:     "logging",
				Version:  "1.x",
				Required: true,
			}},
		},
	}
	result := internalextension.ShowResult{
		Extension: internalextension.Object[internalextension.Extension]{
			Value: internalextension.Extension{
				Metadata: internalextension.ObjectMeta{Name: "demo"},
			},
		},
		SelectedVersion: &internalextension.Object[internalextension.ExtensionVersion]{
			Value: version,
		},
	}
	result.SelectedVersion.Value.Metadata.Labels = nil
	var output bytes.Buffer
	if err := printShow(&output, result); err != nil {
		t.Fatalf("printShow() error = %v", err)
	}
	wantOrder := []string{
		"Name",
		"Extension",
		"Version",
		"Category",
		"Installation Mode",
		"Namespace",
		"KubeSphere Version",
		"Kubernetes Version",
		"Chart URL",
		"Dependencies",
	}
	last := -1
	for _, field := range wantOrder {
		index := strings.Index(output.String(), field)
		if index <= last {
			t.Fatalf("field %q out of order in %q", field, output.String())
		}
		last = index
	}
	if !strings.Contains(output.String(), "Version             v1.0.0+build\n") ||
		!strings.Contains(output.String(), "Extension           demo\n") ||
		!strings.Contains(output.String(), "logging") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestTableCellEscapesTerminalControlSequences(t *testing.T) {
	got := tableCell("safe\x1b]52;c;secret\a\tline\n\u009b")
	for _, unsafe := range []string{"\x1b", "\a", "\t", "\n", "\u009b"} {
		if strings.Contains(got, unsafe) {
			t.Fatalf("tableCell() = %q, contains unsafe %q", got, unsafe)
		}
	}
	if !strings.Contains(got, "safe^[]52;c;secret") {
		t.Fatalf("tableCell() = %q, want visible escaped terminal data", got)
	}
}

func TestOutputVersionsAndNamedStatus(t *testing.T) {
	versions := internalextension.VersionsResult{
		Items: internalextension.List[internalextension.ExtensionVersion]{
			Items: []internalextension.Object[internalextension.ExtensionVersion]{
				{Value: internalextension.ExtensionVersion{
					Spec: internalextension.ExtensionVersionSpec{
						Version:          "1.2.0",
						InstallationMode: "Multicluster",
						KSVersion:        ">=4",
						KubeVersion:      ">=1.27",
						Namespace:        "demo-system",
					},
				}},
			},
		},
	}
	var versionOutput bytes.Buffer
	if err := printVersions(&versionOutput, versions); err != nil {
		t.Fatalf("printVersions() error = %v", err)
	}
	if got, want := versionOutput.String(),
		"VERSION  MODE          KS-VERSION  KUBE-VERSION  NAMESPACE\n"+
			"1.2.0    Multicluster  >=4         >=1.27        demo-system\n"; got != want {
		t.Fatalf("versions = %q, want %q", got, want)
	}

	plan := internalextension.InstallPlan{
		Metadata: internalextension.ObjectMeta{Name: "demo"},
		Spec: internalextension.InstallPlanSpec{
			Extension: internalextension.ExtensionRef{
				Name:    "demo",
				Version: "1.2.0",
			},
			Enabled: true,
		},
		Status: internalextension.InstallPlanStatus{
			InstallationStatus: internalextension.InstallationStatus{
				State:           "Installed",
				Version:         "1.2.0",
				TargetNamespace: "demo-system",
				JobName:         "host-job",
			},
			Enabled: boolPointer(true),
			ClusterSchedulingStatuses: map[string]internalextension.InstallationStatus{
				"member-z": {
					State:           "Installing",
					Version:         "1.2.0",
					TargetNamespace: "demo-system",
					JobName:         "z-job",
				},
				"member-a": {
					State:           "Installed",
					Version:         "1.2.0",
					TargetNamespace: "demo-system",
					JobName:         "a-job",
				},
			},
		},
	}
	result := internalextension.StatusResult{
		Object: &internalextension.Object[internalextension.InstallPlan]{
			Value: plan,
		},
	}
	var statusOutput bytes.Buffer
	if err := printStatus(&statusOutput, result); err != nil {
		t.Fatalf("printStatus() error = %v", err)
	}
	if got, want := statusOutput.String(),
		"NAME           VERSION  ENABLED  STATE       NAMESPACE    JOB\n"+
			"demo           1.2.0    true     Installed   demo-system  host-job\n"+
			"demo/member-a  1.2.0    <none>   Installed   demo-system  a-job\n"+
			"demo/member-z  1.2.0    <none>   Installing  demo-system  z-job\n"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestOutputPropagatesWriterFailures(t *testing.T) {
	sentinel := errors.New("closed")
	if err := writeStructured(
		failingWriter{err: sentinel},
		rawResult(`{}`),
		outputJSON,
	); !errors.Is(err, sentinel) {
		t.Fatalf("writeStructured() error = %v", err)
	}
	if err := printList(
		failingWriter{err: sentinel},
		internalextension.ListResult{},
		outputTable,
	); !errors.Is(err, sentinel) {
		t.Fatalf("printList() error = %v", err)
	}
}

func TestOutputFormattingHelpers(t *testing.T) {
	if got := scalar(""); got != "<none>" {
		t.Fatalf("scalar(\"\") = %q", got)
	}
	if got := localized(map[string]string{
		"zh": "中文",
		"en": "English",
	}); got != "English" {
		t.Fatalf("localized() = %q", got)
	}
	if got := localized(map[string]string{
		"fr": "Français",
		"de": "Deutsch",
	}); got != "Deutsch" {
		t.Fatalf("localized(fallback) = %q", got)
	}
}
