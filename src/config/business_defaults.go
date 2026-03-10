package config

import "reflect"

func (s SystemConfig) applyBusinessDefaults(b BusinessConfig) BusinessConfig {
	if b.Receivers == nil {
		b.Receivers = map[string]ReceiverConfig{}
	}
	if b.Senders == nil {
		b.Senders = map[string]SenderConfig{}
	}
	if b.Tasks == nil {
		b.Tasks = map[string]TaskConfig{}
	}
	for name, rc := range b.Receivers {
		mergeZeroFields(&rc, s.BusinessDefaults.Receiver)
		b.Receivers[name] = rc
	}
	for name, sc := range b.Senders {
		mergeZeroFields(&sc, s.BusinessDefaults.Sender)
		b.Senders[name] = sc
	}
	for name, tc := range b.Tasks {
		mergeZeroFields(&tc, s.BusinessDefaults.Task)
		b.Tasks[name] = tc
	}
	return b
}

func mergeZeroFields(dst any, defaults any) {
	dv := reflect.ValueOf(dst)
	if dv.Kind() != reflect.Ptr || dv.IsNil() {
		return
	}
	dv = dv.Elem()
	sv := reflect.ValueOf(defaults)
	if !dv.IsValid() || !sv.IsValid() || dv.Type() != sv.Type() || dv.Kind() != reflect.Struct {
		return
	}
	for i := 0; i < dv.NumField(); i++ {
		f := dv.Field(i)
		if !f.CanSet() {
			continue
		}
		if !f.IsZero() {
			continue
		}
		d := sv.Field(i)
		if d.IsZero() {
			continue
		}
		f.Set(d)
	}
}
