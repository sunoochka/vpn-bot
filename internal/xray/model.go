package xray

type Config struct {
	Inbounds []Inbound `json:"inbounds"`
}

type Inbound struct {
	Tag	  string `json:"tag"`
	Settings InboundSettings `json:"settings"`
}

type InboundSettings struct {
	Clients []Client `json:"clients"`
}

type Client struct {
	ID string `json:"id"`
	Flow string `json:"flow"`
}