package mockapp

type MockAppClient struct {
	inprocessApp *mockApplication
}

func (client *MockAppClient) StartApp() error {
	return nil
}

func (client *MockAppClient) GetAppAddress() string {
	return client.inprocessApp.listener.Addr().String()
}
