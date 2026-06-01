# Contributing to Axons

First off, thank you for considering contributing to Axons! It's people like you that make Axons such a great tool.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [How Can I Contribute?](#how-can-i-contribute)
  - [Reporting Bugs](#reporting-bugs)
  - [Suggesting Enhancements](#suggesting-enhancements)
  - [Pull Requests](#pull-requests)
- [Development Setup](#development-setup)
  - [Prerequisites](#prerequisites)
  - [Getting Started](#getting-started)
- [Development Workflow](#development-workflow)
  - [Branch Naming](#branch-naming)
  - [Commit Messages](#commit-messages)
  - [Code Style](#code-style)
- [Project Structure](#project-structure)

## Code of Conduct

This project and everyone participating in it is governed by our [Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior to the project maintainers.

## How Can I Contribute?

### Reporting Bugs

Before creating bug reports, please check the issue list as you might find out that you don't need to create one. When you are creating a bug report, please include as many details as possible:

- **Use a clear and descriptive title**
- **Describe the exact steps to reproduce the problem**
- **Provide specific examples to demonstrate the steps**
- **Describe the behavior you observed and expected**
- **Include screenshots or animated GIFs if helpful**
- **Include your environment details** (OS, Go version, Node version, etc.)

### Suggesting Enhancements

Enhancement suggestions are tracked as GitHub issues. When creating an enhancement suggestion, include:

- **Use a clear and descriptive title**
- **Provide a step-by-step description of the suggested enhancement**
- **Provide specific examples to demonstrate the steps**
- **Describe the current behavior and expected behavior**
- **Explain why this enhancement would be useful**

### Pull Requests

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Make your changes
4. Run tests and linting (`make check`)
5. Commit your changes (`git commit -m 'Add some amazing feature'`)
6. Push to the branch (`git push origin feature/amazing-feature`)
7. Open a Pull Request

## Development Setup

### Prerequisites

- **Go**: Version 1.25.0 or higher
- **Node.js**: Version 22 or higher (for frontend development)
- **Make**: Build automation tool
- **Git**: Version control system

### Getting Started

1. **Fork and clone the repository**

   ```bash
   git clone https://github.com/mengshi02/axons.git
   cd axons
   ```

2. **Install Go dependencies**

   ```bash
   make deps
   ```

3. **Install frontend dependencies**

   ```bash
   make frontend-deps
   ```

4. **Build the project**

   ```bash
   make build
   ```

5. **Run the development server**

   ```bash
   make dev
   ```

## Development Workflow

### Branch Naming

Use the following prefixes for branch names:

- `feature/` - New features (e.g., `feature/add-python-support`)
- `fix/` - Bug fixes (e.g., `fix/memory-leak`)
- `docs/` - Documentation changes (e.g., `docs/update-readme`)
- `refactor/` - Code refactoring (e.g., `refactor/improve-performance`)
- `test/` - Adding or updating tests (e.g., `test/add-unit-tests`)

### Commit Messages

Follow the [Conventional Commits](https://www.conventionalcommits.org/) specification:

- `feat:` - A new feature
- `fix:` - A bug fix
- `docs:` - Documentation only changes
- `style:` - Changes that do not affect the meaning of the code
- `refactor:` - A code change that neither fixes a bug nor adds a feature
- `test:` - Adding missing tests or correcting existing tests
- `chore:` - Changes to the build process or auxiliary tools

Example: `feat: add support for Python language parsing`

### Code Style

#### Go Code

- Follow [Effective Go](https://golang.org/doc/effective_go) guidelines
- Run `gofmt` before committing
- Run `go vet` to catch common mistakes
- Use `golangci-lint` for comprehensive linting

Run linting with:

```bash
make lint
```

#### Frontend Code (TypeScript/React)

- Follow the existing code style
- Use ESLint for linting
- Format code with Prettier

Format code with:

```bash
cd ui && npm run lint
```

## Project Structure

```
axons/
├── cmd/axons/          # Application entry point and CLI commands
├── internal/           # Private application code
│   ├── agent/          # AI agent implementations (ReAct agent, profiles, tools)
│   ├── algorithms/     # Core algorithms (Louvain, PageRank)
│   ├── analysis/       # Code analysis logic
│   ├── api/            # HTTP API handlers and MCP server
│   ├── cce/            # Cognitive Context Engine (bimodal embedding, retrieval)
│   ├── client/         # Internal client utilities
│   ├── config/         # Configuration management
│   ├── core/           # Core domain services (build, query, search, audit, etc.)
│   ├── daemon/         # Daemon process management
│   ├── db/             # Database layer (SQLite, repository)
│   ├── extractors/     # Language-specific tree-sitter extractors (8+ languages)
│   ├── graph/          # Graph pipeline, query, and watcher
│   ├── logger/         # Logging utilities
│   ├── mcp/            # MCP protocol implementation (30+ tools)
│   ├── registry/       # Project registry management
│   ├── service/        # Search and embedding services
│   ├── task/           # Async task management
│   ├── terminal/       # Built-in terminal (PTY, WebSocket)
│   └── utils/          # Utility functions
├── pkg/                # Public packages
│   ├── clients/        # External client packages (embedding, reranker)
│   └── types/          # Public type definitions
├── skills/             # Skill packages (Agent Skills specification)
├── ui/                 # Frontend application (React 19 + Sigma.js + xterm.js)
│   ├── src/            # Source code
│   └── public/         # Static assets
├── desktop/            # Desktop application (Wails v3)
├── docs/               # Documentation
├── Makefile            # Build automation
└── go.mod              # Go module definition
```

## Questions?

- **Website**: [axons.chat](https://www.axons.chat)
- **Email**: [support@axons.chat](mailto:support@axons.chat)
- **Bug Reports & Feature Requests**: Open a [GitHub Issue](https://github.com/mengshi02/axons/issues)
- **Questions & Discussions**: Use [GitHub Discussions](https://github.com/mengshi02/axons/discussions)
- **Security Issues**: See our [Security Policy](SECURITY.md)
- **Project Maintainers**: [@mengshi02](https://github.com/mengshi02)

Thank you for your contributions! 🎉