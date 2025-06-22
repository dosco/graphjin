# GraphJin Cursor Rules

This directory contains the new Cursor rules using the [MDC format](https://docs.cursor.com/context/rules) as recommended by Cursor.

## Rules Structure

### Always Applied Rules
- **`graphjin-project.mdc`** - Core project overview and development guidelines (always included)

### Auto-Attached Rules
These rules are automatically included when working with matching file patterns:

- **`core-compiler.mdc`** - GraphQL to SQL compiler guidelines
  - Attached when: `core/**/*.go`, `core/**/*.sql`
  
- **`service-layer.mdc`** - Service layer development guidelines  
  - Attached when: `serv/**/*.go`, `serv/**/*.js`, `serv/**/*.json`

### Agent-Requested Rules
These rules are available to the AI agent to include when relevant:

- **`testing-guidelines.mdc`** - Comprehensive testing guidelines
- **`build-deploy.mdc`** - Build system and deployment guidelines

### Nested Rules
- **`core/internal-packages.mdc`** - Guidelines for core internal packages
  - Attached when: `core/internal/**/*.go`

## Rule Types

Based on the [Cursor documentation](https://docs.cursor.com/context/rules), we use:

- **Always** (`alwaysApply: true`) - Always included in model context
- **Auto Attached** (`globs: [...]`) - Included when files matching patterns are referenced  
- **Agent Requested** (`alwaysApply: false`, no globs) - Available for AI to include when relevant
- **Manual** - Only included when explicitly mentioned using @ruleName

## Migration from Legacy Format

This replaces the legacy `.cursorrules` file with the new structured approach that provides:

- Better organization and modularity
- Automatic attachment based on file patterns
- Nested rules for specific areas of the codebase
- Agent-accessible rules for contextual guidance

## Usage

Rules are automatically applied based on their configuration. You can also:

- Reference rules manually using `@ruleName` in chat
- Use the context picker to see which rules are active
- Generate new rules using `/Generate Cursor Rules` command

For more information, see the [official Cursor rules documentation](https://docs.cursor.com/context/rules). 