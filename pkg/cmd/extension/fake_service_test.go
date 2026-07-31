package extension

import (
	"context"
	"fmt"

	kubesphereextension "github.com/kubesphere/ksctl/pkg/kubesphere/extension"
)

type fakeService struct {
	listFn      func(context.Context, kubesphereextension.ListOptions) (kubesphereextension.ListResult, error)
	showFn      func(context.Context, string, string) (kubesphereextension.ShowResult, error)
	versionsFn  func(context.Context, string) (kubesphereextension.VersionsResult, error)
	statusFn    func(context.Context, string) (kubesphereextension.StatusResult, error)
	watchFn     func(context.Context, string, kubesphereextension.PollOptions) (kubesphereextension.Object[kubesphereextension.InstallPlan], error)
	installFn   func(context.Context, string, kubesphereextension.InstallOptions) (kubesphereextension.Operation, error)
	upgradeFn   func(context.Context, string, kubesphereextension.UpgradeOptions) (kubesphereextension.Operation, error)
	configureFn func(context.Context, string, kubesphereextension.PlanChanges) (kubesphereextension.Operation, error)
	uninstallFn func(context.Context, string) (kubesphereextension.Operation, error)
	waitFn      func(context.Context, kubesphereextension.Operation, kubesphereextension.PollOptions) (kubesphereextension.WaitResult, error)
	diagnoseFn  func(context.Context, string, kubesphereextension.DiagnoseOptions) (kubesphereextension.Diagnosis, error)
}

func (f *fakeService) List(
	ctx context.Context,
	options kubesphereextension.ListOptions,
) (kubesphereextension.ListResult, error) {
	if f.listFn == nil {
		return kubesphereextension.ListResult{}, fmt.Errorf("unexpected List call")
	}
	return f.listFn(ctx, options)
}

func (f *fakeService) Show(
	ctx context.Context,
	name string,
	version string,
) (kubesphereextension.ShowResult, error) {
	if f.showFn == nil {
		return kubesphereextension.ShowResult{}, fmt.Errorf("unexpected Show call")
	}
	return f.showFn(ctx, name, version)
}

func (f *fakeService) Versions(
	ctx context.Context,
	name string,
) (kubesphereextension.VersionsResult, error) {
	if f.versionsFn == nil {
		return kubesphereextension.VersionsResult{}, fmt.Errorf("unexpected Versions call")
	}
	return f.versionsFn(ctx, name)
}

func (f *fakeService) Status(
	ctx context.Context,
	name string,
) (kubesphereextension.StatusResult, error) {
	if f.statusFn == nil {
		return kubesphereextension.StatusResult{}, fmt.Errorf("unexpected Status call")
	}
	return f.statusFn(ctx, name)
}

func (f *fakeService) Watch(
	ctx context.Context,
	name string,
	options kubesphereextension.PollOptions,
) (kubesphereextension.Object[kubesphereextension.InstallPlan], error) {
	if f.watchFn == nil {
		return kubesphereextension.Object[kubesphereextension.InstallPlan]{}, fmt.Errorf(
			"unexpected Watch call",
		)
	}
	return f.watchFn(ctx, name, options)
}

func (f *fakeService) Install(
	ctx context.Context,
	name string,
	options kubesphereextension.InstallOptions,
) (kubesphereextension.Operation, error) {
	if f.installFn == nil {
		return kubesphereextension.Operation{}, fmt.Errorf("unexpected Install call")
	}
	return f.installFn(ctx, name, options)
}

func (f *fakeService) Upgrade(
	ctx context.Context,
	name string,
	options kubesphereextension.UpgradeOptions,
) (kubesphereextension.Operation, error) {
	if f.upgradeFn == nil {
		return kubesphereextension.Operation{}, fmt.Errorf("unexpected Upgrade call")
	}
	return f.upgradeFn(ctx, name, options)
}

func (f *fakeService) Configure(
	ctx context.Context,
	name string,
	changes kubesphereextension.PlanChanges,
) (kubesphereextension.Operation, error) {
	if f.configureFn == nil {
		return kubesphereextension.Operation{}, fmt.Errorf("unexpected Configure call")
	}
	return f.configureFn(ctx, name, changes)
}

func (f *fakeService) Uninstall(
	ctx context.Context,
	name string,
) (kubesphereextension.Operation, error) {
	if f.uninstallFn == nil {
		return kubesphereextension.Operation{}, fmt.Errorf("unexpected Uninstall call")
	}
	return f.uninstallFn(ctx, name)
}

func (f *fakeService) Wait(
	ctx context.Context,
	operation kubesphereextension.Operation,
	options kubesphereextension.PollOptions,
) (kubesphereextension.WaitResult, error) {
	if f.waitFn == nil {
		return kubesphereextension.WaitResult{}, fmt.Errorf("unexpected Wait call")
	}
	return f.waitFn(ctx, operation, options)
}

func (f *fakeService) Diagnose(
	ctx context.Context,
	name string,
	options kubesphereextension.DiagnoseOptions,
) (kubesphereextension.Diagnosis, error) {
	if f.diagnoseFn == nil {
		return kubesphereextension.Diagnosis{}, fmt.Errorf("unexpected Diagnose call")
	}
	return f.diagnoseFn(ctx, name, options)
}
