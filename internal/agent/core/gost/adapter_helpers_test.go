package gost

// serviceNames is the set of every rendered service name, which the settle gate
// uses to compare what the child actually bound.
func serviceNames(config Config) map[string]bool {
	names := map[string]bool{}
	for _, service := range config.Services {
		names[service.Name] = true
	}
	return names
}
