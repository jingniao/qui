package tuning

func readDesired(directory string) (Desired, error) {
	root, err := requestRoot(directory)
	if err != nil {
		return Desired{}, err
	}
	defer root.Close()
	return readDesiredRoot(root)
}
