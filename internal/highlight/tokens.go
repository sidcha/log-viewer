package highlight

import (
	"fmt"
	"slices"
)

// Expand rebuilds p.Rules from p.Items, replacing each use with the rules of
// the named token set, recursively. It runs after every file is read, so a
// set redefined by the user changes every profile that uses it.
func Expand(p *Profile, sets map[string]*TokenSet) error {
	rules, err := expandItems(nil, p.Items, sets, nil)
	if err != nil {
		return fmt.Errorf("profile %s: %w", p.Name, err)
	}
	p.Rules = rules
	return nil
}

func expandItems(dst []Rule, items []Item, sets map[string]*TokenSet, using []string) ([]Rule, error) {
	for _, it := range items {
		if it.Use == "" {
			dst = append(dst, it.Rule)
			continue
		}
		if slices.Contains(using, it.Use) {
			return nil, fmt.Errorf("%s: tokens %s uses itself", it.Where, it.Use)
		}
		set, ok := sets[it.Use]
		if !ok {
			return nil, fmt.Errorf("%s: use of undefined tokens %q", it.Where, it.Use)
		}
		var err error
		dst, err = expandItems(dst, set.Items, sets, append(using, it.Use))
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}
