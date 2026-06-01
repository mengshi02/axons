# Security Policy

## Supported Versions

We actively support the following releases:

- **Latest stable release** - Full support with security updates
- **Previous minor version** - Critical security fixes only
- **Older versions** - No longer supported

We recommend always using the latest release. Check our [releases page](https://github.com/mengshi02/axons/releases) for current versions.

## Reporting a Vulnerability

We take the security of Axons seriously. If you have discovered a security vulnerability, we appreciate your help in disclosing it to us in a responsible manner.

### How to Report

**Please do not report security vulnerabilities through public GitHub issues.**

Instead, please report them using one of the following methods:

1. **GitHub Security Advisories** (Preferred)
   
   Go to the [Security Advisories](https://github.com/mengshi02/axons/security/advisories) page and click "Report a vulnerability".

2. **Email**
   
   Send an email to [support@axons.chat](mailto:support@axons.chat) with details about the vulnerability.

### What to Include

When reporting a vulnerability, please include:

- **Description**: A clear description of the vulnerability
- **Impact**: What could an attacker achieve by exploiting this vulnerability
- **Reproduction**: Step-by-step instructions to reproduce the issue
- **Proof of Concept**: If applicable, a minimal example demonstrating the vulnerability
- **Suggested Fix**: If you have ideas on how to fix the issue

### Response Timeline

- **Initial Response**: We will acknowledge receipt of your report within 48 hours
- **Assessment**: We will assess the vulnerability and determine its severity within 7 days
- **Fix**: We will work on a fix and coordinate the disclosure with you
- **Disclosure**: After the fix is released, we will publish a security advisory

### Disclosure Policy

- We follow the principle of **Coordinated Vulnerability Disclosure**
- We ask that you give us reasonable time to investigate and fix the vulnerability before disclosing it publicly
- We will credit you in the security advisory (unless you prefer to remain anonymous)

## Security Best Practices

When using Axons, please follow these security best practices:

1. **Access Control**: Restrict access to the Axons daemon port to trusted networks
2. **Code Scanning**: Be cautious when indexing untrusted codebases
3. **Updates**: Keep Axons updated to the latest version
4. **Configuration**: Review and configure security settings appropriately

## Security Features

Axons includes the following security features:

- **Path Traversal Protection**: Prevents access to files outside the indexed directories
- **Input Validation**: All API inputs are validated
- **Sandboxed Parsing**: Code parsing is done in a controlled environment

## Contact

For security concerns:
- **Website**: [axons.chat](https://www.axons.chat)
- **Security Issues**: Use [GitHub Security Advisories](https://github.com/mengshi02/axons/security/advisories)
- **Email**: [support@axons.chat](mailto:support@axons.chat)
- **General Questions**: Open a [GitHub Issue](https://github.com/mengshi02/axons/issues)
- **Project Maintainers**: [@mengshi02](https://github.com/mengshi02)

Thank you for helping keep Axons and its users safe! 🔒