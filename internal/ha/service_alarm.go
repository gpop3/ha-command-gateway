package ha

import "strings"

type ServiceAlarm struct{ serviceBase }

func NewServiceAlarm(c *Client) *ServiceAlarm {
	return &ServiceAlarm{newServiceBase("alarm_control_panel", c, map[string]VerbeConfig{
		"arme":      {Action: "alarm_arm_away", Params: []string{"nuit", "dodo", "presence", "vacances", "absent"}},
		"désarme":   {Action: "alarm_disarm"},
		"désactive": {Action: "alarm_disarm"},
	})}
}

func (s *ServiceAlarm) EstActionParDefaut() bool { return false }

func (s *ServiceAlarm) ExtraireParams(texte string) map[string]interface{} {
	params := s.serviceBase.ExtraireParams(texte)
	if strings.Contains(texte, "nuit") || strings.Contains(texte, "dodo") {
		params["arm_mode"] = "alarm_arm_night"
	} else if strings.Contains(texte, "présence") {
		params["arm_mode"] = "alarm_arm_home"
	} else if strings.Contains(texte, "vacances") || strings.Contains(texte, "absent") {
		params["arm_mode"] = "alarm_arm_vacation"
	}
	return params
}

func (s *ServiceAlarm) ExecuterCommande(app Appareil, verbe string, params map[string]interface{}) (string, error) {
	if mode, ok := params["arm_mode"].(string); ok {
		haParams := map[string]interface{}{}
		if code, ok := params["code"].(string); ok {
			haParams["code"] = code
		}
		return s.appeler(app.EntityID, mode, haParams)
	}
	action, ok := s.Verbe(verbe)
	if !ok {
		action = "alarm_arm_away"
	}
	haParams := map[string]interface{}{}
	if code, ok := params["code"].(string); ok {
		haParams["code"] = code
	}
	return s.appeler(app.EntityID, action, haParams)
}

func (s *ServiceAlarm) MotsReconnus() []string {
	return append(s.Verbes(),
		"alarme",
	)
}
