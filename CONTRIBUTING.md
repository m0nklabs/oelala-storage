# Contributing to oelala-storage

Thank you for your interest in contributing to oelala-storage! This guide will help you get started.

## Development Setup

### Prerequisites

- Go 1.24 or later
- Git
- golangci-lint (for linting)

### Getting Started

1. Fork the repository
2. Clone your fork:
   ```bash
   git clone https://github.com/YOUR_USERNAME/oelala-storage.git
   cd oelala-storage
   ```
3. Install dependencies:
   ```bash
   go mod download
   ```
4. Build the project:
   ```bash
   make build
   ```

## Development Workflow

### Running Tests

Before submitting a pull request, ensure all tests pass:

```bash
# Run all tests
make test

# Run tests with coverage
make test-coverage

# Run tests with race detection
go test -race ./...
```

### Linting

We use `golangci-lint` to maintain code quality. Run it before submitting:

```bash
make lint
```

Fix any issues reported by the linter. The CI pipeline will reject PRs with linting errors.

### Code Style

- Follow standard Go conventions
- Use `gofmt` and `goimports` to format code
- Write clear, descriptive commit messages
- Add tests for new functionality
- Update documentation for user-facing changes

### Building

Build for your platform:
```bash
make build
```

Build for all platforms:
```bash
make build-all
```

## Submitting Changes

### Pull Request Process

1. Create a feature branch:
   ```bash
   git checkout -b feature/your-feature-name
   ```

2. Make your changes and commit:
   ```bash
   git add .
   git commit -m "Add feature: description"
   ```

3. Push to your fork:
   ```bash
   git push origin feature/your-feature-name
   ```

4. Open a pull request against the `main` branch

### PR Requirements

Your PR must pass all CI checks:

- ✅ **Tests**: All tests must pass
- ✅ **Linting**: No linting errors
- ✅ **Build**: Must build successfully for all platforms
- ✅ **Coverage**: Should not decrease test coverage significantly

The CI pipeline runs automatically on every PR. Check the Actions tab for results.

### Commit Message Format

Use clear, descriptive commit messages:

```
type: brief description

Longer explanation if needed.

Fixes #123
```

Types:
- `feat`: New feature
- `fix`: Bug fix
- `docs`: Documentation changes
- `test`: Test additions or changes
- `refactor`: Code refactoring
- `perf`: Performance improvements
- `chore`: Build process or auxiliary tool changes

## Testing Guidelines

### Writing Tests

- Place tests in `*_test.go` files
- Use table-driven tests where appropriate
- Test both success and error cases
- Use meaningful test names

Example:
```go
func TestFeature(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {
            name:  "valid input",
            input: "test",
            want:  "test",
        },
        // ... more test cases
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Feature(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Feature() error = %v, wantErr %v", err, tt.wantErr)
                return
            }
            if got != tt.want {
                t.Errorf("Feature() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

## Platform Support

We support:
- ✅ Linux (amd64, arm64)
- ✅ Windows (amd64)
- ✅ Android (arm64)

We **do not** support:
- ❌ macOS
- ❌ iOS

Please do not submit PRs adding support for unsupported platforms.

## Getting Help

- Open an issue for bugs or feature requests
- Check existing issues before creating a new one
- Be respectful and constructive

## License

By contributing, you agree that your contributions will be licensed under the MIT License.
