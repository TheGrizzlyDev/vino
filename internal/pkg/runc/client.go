package runc

type Command interface {
	Subcommand() string
	Groups() []string
}

type Client interface {
	private()
	Run(command Command) error
}

func NewDelegatingCliClient(delegatePath string) (Client, error) {
	return delegatingCliClient{delegate: delegatePath}, nil
}

type delegatingCliClient struct {
	delegate string
}

func (d delegatingCliClient) private() {}

func (d delegatingCliClient) Run(command Command) error {
	return nil
}
