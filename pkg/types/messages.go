package types

type MessageDetails struct {
	Texte  string        `json:"texte"`
	Params []interface{} `json:"params"`
}

type Message struct {
	SMS  MessageDetails `json:"sms"`
	Voix MessageDetails `json:"voix"`
}
