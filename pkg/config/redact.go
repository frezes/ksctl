package config

const redactedValue = "<redacted>"

func RedactedCopy(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}

	copy := *cfg
	copy.Fleets = make(map[string]Fleet, len(cfg.Fleets))
	for name, fleet := range cfg.Fleets {
		fleet.TLSClientConfig.NextProtos = append([]string(nil), fleet.TLSClientConfig.NextProtos...)
		if fleet.TLSClientConfig.KeyData != "" {
			fleet.TLSClientConfig.KeyData = redactedValue
		}
		users := make(map[string]User, len(fleet.Users))
		for userName, user := range fleet.Users {
			if user.BearerToken != "" {
				user.BearerToken = redactedValue
			}
			if user.Password != "" {
				user.Password = redactedValue
			}
			users[userName] = user
		}
		fleet.Users = users
		copy.Fleets[name] = fleet
	}

	copy.Contexts = make(map[string]Context, len(cfg.Contexts))
	for name, context := range cfg.Contexts {
		copy.Contexts[name] = context
	}
	return &copy
}
