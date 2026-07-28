package extension

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func (s *Service) List(ctx context.Context, options ListOptions) (ListResult, error) {
	extensions, err := s.client.ListExtensions(ctx, options.Category)
	if err != nil {
		return ListResult{}, err
	}
	plans, err := s.client.ListInstallPlans(ctx)
	if err != nil {
		return ListResult{}, err
	}

	plansByName := make(map[string]Object[InstallPlan], len(plans.Items))
	for _, plan := range plans.Items {
		plansByName[plan.Value.Metadata.Name] = plan
	}

	sort.SliceStable(extensions.Items, func(i, j int) bool {
		return extensions.Items[i].Value.Metadata.Name <
			extensions.Items[j].Value.Metadata.Name
	})

	items := make([]ListItem, 0, len(extensions.Items))
	rawItems := make([]Object[Extension], 0, len(extensions.Items))
	for _, extension := range extensions.Items {
		plan, found := plansByName[extension.Value.Metadata.Name]
		if options.InstalledOnly && (!found || !countsAsInstalled(plan.Value)) {
			continue
		}
		item := ListItem{Extension: extension}
		if found {
			planCopy := plan
			item.InstallPlan = &planCopy
		}
		items = append(items, item)
		rawItems = append(rawItems, extension)
	}

	filtered, err := replaceListItems(extensions, rawItems)
	if err != nil {
		return ListResult{}, fmt.Errorf("build extension list output: %w", err)
	}
	return ListResult{Items: items, raw: filtered}, nil
}

func countsAsInstalled(plan InstallPlan) bool {
	return plan.Metadata.DeletionTimestamp == nil && plan.Status.State != "Uninstalled"
}

func (s *Service) Show(
	ctx context.Context,
	name string,
	requestedVersion string,
) (ShowResult, error) {
	if err := validatePathName("extension", name); err != nil {
		return ShowResult{}, err
	}
	extension, err := s.client.GetExtension(ctx, name)
	if err != nil {
		return ShowResult{}, err
	}
	versions, err := s.sortedVersions(ctx, name)
	if err != nil {
		return ShowResult{}, err
	}

	result := ShowResult{Extension: extension, Versions: versions}
	if requestedVersion != "" {
		selected, err := findExactVersion(name, requestedVersion, versions)
		if err != nil {
			return ShowResult{}, err
		}
		result.SelectedVersion = &selected
		return result, nil
	}

	plan, err := s.client.GetInstallPlan(ctx, name)
	if err == nil {
		result.InstallPlan = &plan
	} else if !apierrors.IsNotFound(err) {
		return ShowResult{}, err
	}
	return result, nil
}

func (s *Service) Versions(ctx context.Context, name string) (VersionsResult, error) {
	if err := validatePathName("extension", name); err != nil {
		return VersionsResult{}, err
	}
	versions, err := s.sortedVersions(ctx, name)
	if err != nil {
		return VersionsResult{}, err
	}
	return VersionsResult{Items: versions}, nil
}

func (s *Service) sortedVersions(ctx context.Context, name string) (List[ExtensionVersion], error) {
	versions, err := s.client.ListExtensionVersions(ctx, name)
	if err != nil {
		return List[ExtensionVersion]{}, err
	}

	parsed := make(map[string]*semver.Version, len(versions.Items))
	allSemantic := true
	for _, item := range versions.Items {
		value, parseErr := semver.NewVersion(item.Value.Spec.Version)
		if parseErr != nil {
			allSemantic = false
			break
		}
		parsed[item.Value.Metadata.Name] = value
	}

	sort.SliceStable(versions.Items, func(i, j int) bool {
		left := versions.Items[i].Value
		right := versions.Items[j].Value
		if allSemantic {
			comparison := parsed[left.Metadata.Name].Compare(parsed[right.Metadata.Name])
			if comparison != 0 {
				return comparison > 0
			}
		}
		if left.Spec.Version != right.Spec.Version {
			return left.Spec.Version > right.Spec.Version
		}
		return left.Metadata.Name < right.Metadata.Name
	})
	sorted, err := replaceListItems(versions, versions.Items)
	if err != nil {
		return List[ExtensionVersion]{}, fmt.Errorf(
			"build versions for extension %q: %w",
			name,
			err,
		)
	}
	return sorted, nil
}

func (s *Service) exactVersion(
	ctx context.Context,
	name string,
	requested string,
) (Object[ExtensionVersion], error) {
	if strings.TrimSpace(requested) == "" {
		return Object[ExtensionVersion]{}, fmt.Errorf("exact extension version is required")
	}
	if err := validatePathName("extension", name); err != nil {
		return Object[ExtensionVersion]{}, err
	}
	versions, err := s.client.ListExtensionVersions(ctx, name)
	if err != nil {
		return Object[ExtensionVersion]{}, err
	}
	return findExactVersion(name, requested, versions)
}

func findExactVersion(
	name string,
	requested string,
	versions List[ExtensionVersion],
) (Object[ExtensionVersion], error) {
	for _, version := range versions.Items {
		if version.Value.Spec.Version == requested {
			return version, nil
		}
	}
	return Object[ExtensionVersion]{}, apierrors.NewNotFound(
		schema.GroupResource{
			Group:    "kubesphere.io",
			Resource: "extensionversions",
		},
		name+"@"+requested,
	)
}

func (s *Service) Status(ctx context.Context, name string) (StatusResult, error) {
	if name != "" {
		if err := validatePathName("extension", name); err != nil {
			return StatusResult{}, err
		}
		plan, err := s.client.GetInstallPlan(ctx, name)
		if err != nil {
			return StatusResult{}, err
		}
		return StatusResult{Object: &plan}, nil
	}

	plans, err := s.client.ListInstallPlans(ctx)
	if err != nil {
		return StatusResult{}, err
	}
	sort.SliceStable(plans.Items, func(i, j int) bool {
		return plans.Items[i].Value.Metadata.Name <
			plans.Items[j].Value.Metadata.Name
	})
	sorted, err := replaceListItems(plans, plans.Items)
	if err != nil {
		return StatusResult{}, fmt.Errorf("build install plan list output: %w", err)
	}
	return StatusResult{List: &sorted}, nil
}
