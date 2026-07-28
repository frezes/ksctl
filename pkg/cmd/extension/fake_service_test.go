package extension

import (
	"context"
	"fmt"

	internalextension "github.com/kubesphere/ksctl/internal/extension"
)

type fakeService struct {
	listFn      func(context.Context, internalextension.ListOptions) (internalextension.ListResult, error)
	showFn      func(context.Context, string, string) (internalextension.ShowResult, error)
	versionsFn  func(context.Context, string) (internalextension.VersionsResult, error)
	statusFn    func(context.Context, string) (internalextension.StatusResult, error)
	watchFn     func(context.Context, string, internalextension.PollOptions) (internalextension.Object[internalextension.InstallPlan], error)
	installFn   func(context.Context, string, internalextension.InstallOptions) (internalextension.Operation, error)
	upgradeFn   func(context.Context, string, internalextension.UpgradeOptions) (internalextension.Operation, error)
	configureFn func(context.Context, string, internalextension.PlanChanges) (internalextension.Operation, error)
	uninstallFn func(context.Context, string) (internalextension.Operation, error)
	waitFn      func(context.Context, internalextension.Operation, internalextension.PollOptions) (internalextension.WaitResult, error)
	diagnoseFn  func(context.Context, string, internalextension.DiagnoseOptions) (internalextension.Diagnosis, error)
}

func (f *fakeService) List(
	ctx context.Context,
	options internalextension.ListOptions,
) (internalextension.ListResult, error) {
	if f.listFn == nil {
		return internalextension.ListResult{}, fmt.Errorf("unexpected List call")
	}
	return f.listFn(ctx, options)
}

func (f *fakeService) Show(
	ctx context.Context,
	name string,
	version string,
) (internalextension.ShowResult, error) {
	if f.showFn == nil {
		return internalextension.ShowResult{}, fmt.Errorf("unexpected Show call")
	}
	return f.showFn(ctx, name, version)
}

func (f *fakeService) Versions(
	ctx context.Context,
	name string,
) (internalextension.VersionsResult, error) {
	if f.versionsFn == nil {
		return internalextension.VersionsResult{}, fmt.Errorf("unexpected Versions call")
	}
	return f.versionsFn(ctx, name)
}

func (f *fakeService) Status(
	ctx context.Context,
	name string,
) (internalextension.StatusResult, error) {
	if f.statusFn == nil {
		return internalextension.StatusResult{}, fmt.Errorf("unexpected Status call")
	}
	return f.statusFn(ctx, name)
}

func (f *fakeService) Watch(
	ctx context.Context,
	name string,
	options internalextension.PollOptions,
) (internalextension.Object[internalextension.InstallPlan], error) {
	if f.watchFn == nil {
		return internalextension.Object[internalextension.InstallPlan]{}, fmt.Errorf(
			"unexpected Watch call",
		)
	}
	return f.watchFn(ctx, name, options)
}

func (f *fakeService) Install(
	ctx context.Context,
	name string,
	options internalextension.InstallOptions,
) (internalextension.Operation, error) {
	if f.installFn == nil {
		return internalextension.Operation{}, fmt.Errorf("unexpected Install call")
	}
	return f.installFn(ctx, name, options)
}

func (f *fakeService) Upgrade(
	ctx context.Context,
	name string,
	options internalextension.UpgradeOptions,
) (internalextension.Operation, error) {
	if f.upgradeFn == nil {
		return internalextension.Operation{}, fmt.Errorf("unexpected Upgrade call")
	}
	return f.upgradeFn(ctx, name, options)
}

func (f *fakeService) Configure(
	ctx context.Context,
	name string,
	changes internalextension.PlanChanges,
) (internalextension.Operation, error) {
	if f.configureFn == nil {
		return internalextension.Operation{}, fmt.Errorf("unexpected Configure call")
	}
	return f.configureFn(ctx, name, changes)
}

func (f *fakeService) Uninstall(
	ctx context.Context,
	name string,
) (internalextension.Operation, error) {
	if f.uninstallFn == nil {
		return internalextension.Operation{}, fmt.Errorf("unexpected Uninstall call")
	}
	return f.uninstallFn(ctx, name)
}

func (f *fakeService) Wait(
	ctx context.Context,
	operation internalextension.Operation,
	options internalextension.PollOptions,
) (internalextension.WaitResult, error) {
	if f.waitFn == nil {
		return internalextension.WaitResult{}, fmt.Errorf("unexpected Wait call")
	}
	return f.waitFn(ctx, operation, options)
}

func (f *fakeService) Diagnose(
	ctx context.Context,
	name string,
	options internalextension.DiagnoseOptions,
) (internalextension.Diagnosis, error) {
	if f.diagnoseFn == nil {
		return internalextension.Diagnosis{}, fmt.Errorf("unexpected Diagnose call")
	}
	return f.diagnoseFn(ctx, name, options)
}
