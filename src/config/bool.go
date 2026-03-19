package config

func BoolValue(v *bool) bool {
	if v == nil {
		return false
	}
	return *v
}
