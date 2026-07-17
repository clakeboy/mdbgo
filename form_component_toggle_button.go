package mdbgo

import "strings"

func parseJet4ToggleButtonTextProperties(control FormControlInfo, fields []jet4TaggedTextField) []FormProperty {
	if len(fields) == 0 || fields[0].Tag != 0xDD {
		return nil
	}
	value := strings.TrimSpace(fields[0].Value)
	if value == "" || strings.EqualFold(value, control.Name) || isKnownFormFont(value) {
		return nil
	}
	return []FormProperty{newTextFormProperty(0x0011, value)}
}
