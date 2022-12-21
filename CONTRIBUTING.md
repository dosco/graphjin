# Contributing to GraphJin

GraphJin is a very approchable code-base and a project that is easy for almost
anyone with basic GO knowledge to start contributing to. It is also a young project
so a lot of high value work is there for the taking.

Even the GraphQL to SQL compiler that is at the heart of GraphJin is essentially a text book compiler with clean and easy to read code. The data structures used by the lexer, parser and sql generator are easy to understand and modify.

Finally we do have a lot of test for critical parts of the codebase which makes it easy for you to modify with confidence. I'm always available for questions or any sort of guidance so feel fee to reach out over twitter or discord.

- [Getting Started](#getting-started)
- [Setting Up the Development Environment](#setup-development-environment)
  - [Prerequisites](#prerequisites)
  - [Get the GraphJin source](#get-source-code)
  - [Start the development envoirnment ](#start-the-development-envoirnment)
  - [Testing](#testing-and-linting)
- [Contributing](#contributing)
  - [Guidelines](#guidelines)
  - [Code style](#code-style)

## Getting Started

- Read the [Getting Started Guide](https://graphjin.com/guide.html#get-started)

## Setup Development Environment

### Prerequisites

- Install [Git](https://git-scm.com/) (may be already installed on your system, or available through your OS package manager)
- Install [Go 1.13 or above](https://golang.org/doc/install)
- Install [Docker](https://docs.docker.com/v17.09/engine/installation/)

### Get source code

The entire build flow uses `Makefile` there is a whole list of sub-commands you
can use to build, test, install, lint, etc.

```bash
git clone https://github.com/dosco/graphjin
cd ./graphjin
make help
```

### Start the development envoirnment

Ensure you have a Postgres database running and the config file has the correct connection details to it.

```bash
go run main.go db setup
go run main.go serve
```

### Learn how the code works

[GraphJin codebase explained](https://graphjin.com/internals.html)

### Testing and Linting

```
make lint
make test
```

## Contributing

### Guidelines

- **Pull requests are welcome**, as long as you're willing to put in the effort to meet the guidelines.
- Aim for clear, well written, maintainable code.
- Simple and minimal approach to features, like Go.
- Refactoring existing code now for better performance, better readability or better testability wins over adding a new feature.
- Don't add a function to a module that you don't use right now, or doesn't clearly enable a planned functionality.
- Don't ship a half done feature, which would require significant alterations to work fully.
- Avoid [Technical debt](https://en.wikipedia.org/wiki/Technical_debt) like cancer.
- Leave the code cleaner than when you began.

### Code style

- We're following [Go Code Review](https://github.com/golang/go/wiki/CodeReviewComments).
- Use `go fmt` to format your code before committing.
- If you see _any code_ which clearly violates the style guide, please fix it and send a pull request. No need to ask for permission.
- Avoid unnecessary vertical spaces. Use your judgment or follow the code review comments.
- Wrap your code and comments to 100 characters, unless doing so makes the code less legible.
