package extension

import (
	"time"
)

type Service struct {
	client       APIClient
	pollInterval time.Duration
	after        func(time.Duration) <-chan time.Time
}

func NewService(client APIClient) *Service {
	return &Service{
		client:       client,
		pollInterval: time.Second,
		after:        time.After,
	}
}

func newServiceForPolling(
	client APIClient,
	interval time.Duration,
	after func(time.Duration) <-chan time.Time,
) *Service {
	return &Service{
		client:       client,
		pollInterval: interval,
		after:        after,
	}
}

type ListOptions struct {
	Category      string
	InstalledOnly bool
}

type ListItem struct {
	Extension   Object[Extension]
	InstallPlan *Object[InstallPlan]
}

type ListResult struct {
	Items []ListItem
	raw   List[Extension]
}

func (r ListResult) RawJSON() []byte {
	return r.raw.RawJSON()
}

type ShowResult struct {
	Extension       Object[Extension]
	Versions        List[ExtensionVersion]
	InstallPlan     *Object[InstallPlan]
	SelectedVersion *Object[ExtensionVersion]
}

func (r ShowResult) RawJSON() []byte {
	if r.SelectedVersion != nil {
		return r.SelectedVersion.RawJSON()
	}
	return r.Extension.RawJSON()
}

type VersionsResult struct {
	Items List[ExtensionVersion]
}

func (r VersionsResult) RawJSON() []byte {
	return r.Items.RawJSON()
}

type StatusResult struct {
	List   *List[InstallPlan]
	Object *Object[InstallPlan]
}

func (r StatusResult) RawJSON() []byte {
	if r.Object != nil {
		return r.Object.RawJSON()
	}
	if r.List != nil {
		return r.List.RawJSON()
	}
	return nil
}
