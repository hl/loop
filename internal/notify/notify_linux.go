package notify

func send(title, body string) error {
	args := notifySendArgs(title, body)
	return run(args[0], args[1:]...)
}
