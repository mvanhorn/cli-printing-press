package pipeline

import "fmt"

var registry = map[string]Phase{}

func Register(p Phase) {
	registry[p.Name()] = p
}

func Get(name string) (Phase, error) {
	p, ok := registry[name]
	if !ok {
		return nil, fmt.Errorf("phase %q not registered", name)
	}
	return p, nil
}

func AllRegistered() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}
