package extension

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	kubesphereextension "kubesphere.io/ksctl/pkg/kubesphere/extension"
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
	extension := kubesphereextension.Extension{
		Metadata: kubesphereextension.ObjectMeta{
			Name: "demo",
			Labels: map[string]string{
				"kubesphere.io/category": "observability",
			},
		},
		Spec: kubesphereextension.ExtensionSpec{
			Provider: map[string]*kubesphereextension.Provider{
				"en": {Name: "KubeSphere"},
			},
		},
		Status: kubesphereextension.ExtensionStatus{
			RecommendedVersion: "1.3.0",
			Enabled:            boolPointer(true),
		},
	}
	plan := kubesphereextension.InstallPlan{
		Metadata: kubesphereextension.ObjectMeta{Name: "demo"},
		Spec: kubesphereextension.InstallPlanSpec{
			Extension: kubesphereextension.ExtensionRef{Name: "demo", Version: "1.2.0"},
			Enabled:   true,
		},
		Status: kubesphereextension.InstallPlanStatus{
			InstallationStatus: kubesphereextension.InstallationStatus{
				State:   "Installed",
				Version: "1.2.0",
			},
		},
	}
	result := kubesphereextension.ListResult{Items: []kubesphereextension.ListItem{{
		Extension: kubesphereextension.Object[kubesphereextension.Extension]{Value: extension},
		InstallPlan: &kubesphereextension.Object[kubesphereextension.InstallPlan]{
			Value: plan,
		},
	}}}

	var table bytes.Buffer
	if err := printList(&table, result, outputTable); err != nil {
		t.Fatalf("printList(table) error = %v", err)
	}
	if got, want := table.String(),
		"NAME  CATEGORY       RECOMMENDED  INSTALLED  STATE\n"+
			"demo  observability  1.3.0        1.2.0      Installed\n"; got != want {
		t.Fatalf("table = %q, want %q", got, want)
	}

	var wide bytes.Buffer
	if err := printList(&wide, result, outputWide); err != nil {
		t.Fatalf("printList(wide) error = %v", err)
	}
	if got, want := wide.String(),
		"NAME  CATEGORY       RECOMMENDED  INSTALLED  STATE      PROVIDER    ENABLED\n"+
			"demo  observability  1.3.0        1.2.0      Installed  KubeSphere  true\n"; got != want {
		t.Fatalf("wide = %q, want %q", got, want)
	}
}

func TestOutputDoesNotReportUnobservedTargetAsInstalled(t *testing.T) {
	extension := kubesphereextension.Extension{
		Metadata: kubesphereextension.ObjectMeta{Name: "demo"},
	}
	plan := kubesphereextension.InstallPlan{
		Metadata: kubesphereextension.ObjectMeta{Name: "demo"},
		Spec: kubesphereextension.InstallPlanSpec{
			Extension: kubesphereextension.ExtensionRef{
				Name:    "demo",
				Version: "2.0.0",
			},
			Enabled: true,
		},
		Status: kubesphereextension.InstallPlanStatus{
			InstallationStatus: kubesphereextension.InstallationStatus{
				State: "Preparing",
			},
		},
	}
	item := kubesphereextension.ListItem{
		Extension: kubesphereextension.Object[kubesphereextension.Extension]{
			Value: extension,
		},
		InstallPlan: &kubesphereextension.Object[kubesphereextension.InstallPlan]{
			Value: plan,
		},
	}

	var listOutput bytes.Buffer
	if err := printList(
		&listOutput,
		kubesphereextension.ListResult{
			Items: []kubesphereextension.ListItem{item},
		},
		outputTable,
	); err != nil {
		t.Fatalf("printList() error = %v", err)
	}
	if !strings.Contains(
		listOutput.String(),
		"demo  <none>    <none>       <none>     Preparing",
	) {
		t.Fatalf("list output = %q", listOutput.String())
	}

	var showOutput bytes.Buffer
	if err := printShow(
		&showOutput,
		kubesphereextension.ShowResult{
			Extension:   item.Extension,
			InstallPlan: item.InstallPlan,
		},
		outputWide,
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
	extension := kubesphereextension.Extension{
		Metadata: kubesphereextension.ObjectMeta{Name: "demo"},
		Status: kubesphereextension.ExtensionStatus{
			State:            "Installing",
			Enabled:          boolPointer(false),
			InstalledVersion: "1.0.0",
		},
	}
	plan := kubesphereextension.InstallPlan{
		Metadata: kubesphereextension.ObjectMeta{Name: "demo"},
		Spec: kubesphereextension.InstallPlanSpec{
			Extension: kubesphereextension.ExtensionRef{
				Name:    "demo",
				Version: "2.0.0",
			},
			Enabled: true,
		},
		Status: kubesphereextension.InstallPlanStatus{
			InstallationStatus: kubesphereextension.InstallationStatus{
				State:   "Upgraded",
				Version: "2.0.0",
			},
			Enabled: boolPointer(true),
		},
	}
	result := kubesphereextension.ShowResult{
		Extension: kubesphereextension.Object[kubesphereextension.Extension]{
			Value: extension,
		},
		InstallPlan: &kubesphereextension.Object[kubesphereextension.InstallPlan]{
			Value: plan,
		},
	}

	var output bytes.Buffer
	if err := printShow(&output, result, outputTable); err != nil {
		t.Fatalf("printShow() error = %v", err)
	}
	for _, want := range []string{
		"State              Upgraded\n",
		"Installed Version  2.0.0\n",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("show output = %q, want %q", output.String(), want)
		}
	}
}

func TestOutputShowDefaultIsConciseAndOmitsEmptyValues(t *testing.T) {
	result := kubesphereextension.ShowResult{
		Extension: kubesphereextension.Object[kubesphereextension.Extension]{
			Value: kubesphereextension.Extension{
				Metadata: kubesphereextension.ObjectMeta{Name: "demo"},
				Spec: kubesphereextension.ExtensionSpec{
					DisplayName: map[string]string{"en": "Demo"},
				},
				Status: kubesphereextension.ExtensionStatus{
					RecommendedVersion: "1.3.0",
				},
			},
		},
		InstallPlan: &kubesphereextension.Object[kubesphereextension.InstallPlan]{
			Value: kubesphereextension.InstallPlan{
				Status: kubesphereextension.InstallPlanStatus{
					InstallationStatus: kubesphereextension.InstallationStatus{
						State:   "Installed",
						Version: "1.2.1",
					},
				},
			},
		},
	}
	var output bytes.Buffer
	if err := printShow(&output, result, outputTable); err != nil {
		t.Fatalf("printShow() error = %v", err)
	}
	for _, want := range []string{
		"Name                 demo\n",
		"Display Name         Demo\n",
		"State                Installed\n",
		"Installed Version    1.2.1\n",
		"Recommended Version  1.3.0\n",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output = %q, want %q", output.String(), want)
		}
	}
	for _, absent := range []string{"Description", "Category", "Provider"} {
		if strings.Contains(output.String(), absent) {
			t.Fatalf("output = %q, unexpectedly contains %q", output.String(), absent)
		}
	}
}

func TestOutputShowWideFieldOrderAndMissingScalars(t *testing.T) {
	extension := kubesphereextension.Extension{
		Metadata: kubesphereextension.ObjectMeta{Name: "demo"},
		Spec: kubesphereextension.ExtensionSpec{
			DisplayName: map[string]string{"zh": "演示", "en": "Demo"},
		},
		Status: kubesphereextension.ExtensionStatus{
			Versions: []kubesphereextension.ExtensionVersionInfo{
				{Version: "1.2.0"},
				{Version: "1.1.0"},
			},
		},
	}
	result := kubesphereextension.ShowResult{
		Extension: kubesphereextension.Object[kubesphereextension.Extension]{
			Value: extension,
		},
	}
	var output bytes.Buffer
	if err := printShow(&output, result, outputWide); err != nil {
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
	version := kubesphereextension.ExtensionVersion{
		Metadata: kubesphereextension.ObjectMeta{
			Name: "demo-v1",
			Labels: map[string]string{
				"kubesphere.io/extension-ref": "demo",
			},
		},
		Spec: kubesphereextension.ExtensionVersionSpec{
			Version:          "v1.0.0+build",
			Category:         "observability",
			InstallationMode: "HostOnly",
			Namespace:        "demo-system",
			KSVersion:        ">=4",
			KubeVersion:      ">=1.27",
			ChartURL:         "oci://example/demo",
			ExternalDependencies: []kubesphereextension.ExternalDependency{{
				Name:     "logging",
				Version:  "1.x",
				Required: true,
			}},
		},
	}
	result := kubesphereextension.ShowResult{
		Extension: kubesphereextension.Object[kubesphereextension.Extension]{
			Value: kubesphereextension.Extension{
				Metadata: kubesphereextension.ObjectMeta{Name: "demo"},
			},
		},
		SelectedVersion: &kubesphereextension.Object[kubesphereextension.ExtensionVersion]{
			Value: version,
		},
	}
	result.SelectedVersion.Value.Metadata.Labels = nil
	var output bytes.Buffer
	if err := printShow(&output, result, outputTable); err != nil {
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

func TestOutputShowPrintsSortedClusterSchedulingStatuses(t *testing.T) {
	result := kubesphereextension.ShowResult{
		Extension: kubesphereextension.Object[kubesphereextension.Extension]{
			Value: kubesphereextension.Extension{
				Metadata: kubesphereextension.ObjectMeta{Name: "demo"},
			},
		},
		InstallPlan: &kubesphereextension.Object[kubesphereextension.InstallPlan]{
			Value: kubesphereextension.InstallPlan{
				Status: kubesphereextension.InstallPlanStatus{
					ClusterSchedulingStatuses: map[string]kubesphereextension.InstallationStatus{
						"member-z": {
							Version:         "1.2.1",
							State:           "Installing",
							TargetNamespace: "demo-system",
							JobName:         "job-z",
						},
						"member-a": {
							Version:         "1.2.1",
							State:           "Installed",
							TargetNamespace: "demo-system",
							JobName:         "job-a",
						},
					},
				},
			},
		},
	}
	var compact bytes.Buffer
	if err := printShow(&compact, result, outputTable); err != nil {
		t.Fatalf("printShow(table) error = %v", err)
	}
	if !strings.Contains(
		compact.String(),
		"clusterSchedulingStatuses\n\nCLUSTER   VERSION  STATE\n"+
			"member-a  1.2.1    Installed\n"+
			"member-z  1.2.1    Installing\n",
	) {
		t.Fatalf("compact output = %q", compact.String())
	}

	var wide bytes.Buffer
	if err := printShow(&wide, result, outputWide); err != nil {
		t.Fatalf("printShow(wide) error = %v", err)
	}
	for _, want := range []string{
		"CLUSTER",
		"VERSION",
		"STATE",
		"NAMESPACE",
		"JOB",
		"member-a",
		"job-a",
		"member-z",
		"job-z",
	} {
		if !strings.Contains(wide.String(), want) {
			t.Fatalf("wide output = %q, want %q", wide.String(), want)
		}
	}
	if strings.Index(wide.String(), "member-a") >
		strings.Index(wide.String(), "member-z") {
		t.Fatalf("wide output is not sorted: %q", wide.String())
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
	versions := kubesphereextension.VersionsResult{
		Items: kubesphereextension.List[kubesphereextension.ExtensionVersion]{
			Items: []kubesphereextension.Object[kubesphereextension.ExtensionVersion]{
				{Value: kubesphereextension.ExtensionVersion{
					Spec: kubesphereextension.ExtensionVersionSpec{
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

	plan := kubesphereextension.InstallPlan{
		Metadata: kubesphereextension.ObjectMeta{Name: "demo"},
		Spec: kubesphereextension.InstallPlanSpec{
			Extension: kubesphereextension.ExtensionRef{
				Name:    "demo",
				Version: "1.2.0",
			},
			Enabled: true,
		},
		Status: kubesphereextension.InstallPlanStatus{
			InstallationStatus: kubesphereextension.InstallationStatus{
				State:           "Installed",
				Version:         "1.2.0",
				TargetNamespace: "demo-system",
				JobName:         "host-job",
			},
			Enabled: boolPointer(true),
			ClusterSchedulingStatuses: map[string]kubesphereextension.InstallationStatus{
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
	result := kubesphereextension.StatusResult{
		Object: &kubesphereextension.Object[kubesphereextension.InstallPlan]{
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
		kubesphereextension.ListResult{},
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
	if got := localizedValue(map[string]string{
		"de": "",
		"fr": "Français",
	}); got != "Français" {
		t.Fatalf("localizedValue() = %q", got)
	}
}
