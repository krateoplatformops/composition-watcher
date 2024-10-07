package compositions

import (
	"regexp"

	watcher "github.com/krateoplatformops/composition-watcher/api/v1"
)

func isFullMatch(pattern, str string) (bool, error) {
	regex, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return regex.MatchString(str), nil
}

func shouldItSkip(exclude watcher.Exclude, managedResource watcher.Reference) bool {
	match := []bool{false, false, false}
	// Check ApiGroup
	if exclude.ApiVersion == "" {
		match[0] = true
	} else if exclude.ApiVersion == managedResource.ApiVersion {
		match[0] = true
	} else { // Check if ApiGroup is regex
		fullMatch, _ := isFullMatch(exclude.ApiVersion, managedResource.ApiVersion)
		if fullMatch {
			match[0] = true
		}
	}

	// Check Resource
	if exclude.Resource == "" {
		match[1] = true
	} else if exclude.Resource == managedResource.Resource {
		match[1] = true
	} else { // Check if ApiGroup is regex
		fullMatch, _ := isFullMatch(exclude.Resource, managedResource.Resource)
		if fullMatch {
			match[1] = true
		}
	}

	// Check Name
	if exclude.Name == "" {
		match[2] = true
	} else if exclude.Name == managedResource.Name {
		match[2] = true
	} else { // Check if ApiGroup is regex
		fullMatch, _ := isFullMatch(exclude.Name, managedResource.Name)
		if fullMatch {
			match[2] = true
		}
	}

	if match[0] && match[1] && match[2] {
		return true
	}
	return false
}
