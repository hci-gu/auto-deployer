package github

type PullRequestEvent struct {
	Action      string `json:"action"`
	Repository  Repo   `json:"repository"`
	PullRequest PR     `json:"pull_request"`
}

type Repo struct {
	FullName    string `json:"full_name"`
	CloneURL    string `json:"clone_url"`
	SSHURL      string `json:"ssh_url"`
	HTMLURL     string `json:"html_url"`
	Private     bool   `json:"private"`
	Archived    bool   `json:"archived"`
	Description string `json:"description"`
	Owner       User   `json:"owner"`
}

type User struct {
	Login string `json:"login"`
}

type PR struct {
	Number int       `json:"number"`
	Head   PRHead    `json:"head"`
	Base   PRBase    `json:"base"`
	Merged bool      `json:"merged"`
	State  string    `json:"state"`
	User   PRUser    `json:"user"`
	URL    string    `json:"html_url"`
	Title  string    `json:"title"`
	Body   string    `json:"body"`
	Draft  bool      `json:"draft"`
	Labels []PRLabel `json:"labels"`
}

type PRHead struct {
	SHA  string `json:"sha"`
	Ref  string `json:"ref"`
	Repo Repo   `json:"repo"`
}

type PRBase struct {
	Ref  string `json:"ref"`
	Repo Repo   `json:"repo"`
}

type PRUser struct {
	Login string `json:"login"`
}

type PRLabel struct {
	Name string `json:"name"`
}
