# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in the Dify2API source code, please
**do not open a public issue**.  Instead, report it privately via GitHub
Security Advisories:

1. Go to the repository's **Security** tab → **Advisories** →
   **New draft security advisory**.
2. Describe the vulnerability, including steps to reproduce and affected
   versions (commit hash or tag).
3. Submit the draft. The maintainer will be notified privately.

Please include:

- A clear description of the vulnerability
- Steps to reproduce (proof-of-concept code or curl commands are helpful)
- Affected versions
- Any known workarounds

**Response timeline**:

- We aim to acknowledge reports within **72 hours**.
- We aim to publish a fix and advisory within **30 days** of confirmation.
- After the fix is released, we will credit the reporter in the advisory
  (unless you prefer to remain anonymous).

## Supported Versions

| Version | Supported          |
|---------|:------------------:|
| 1.x     | ✅ (latest commit) |

The project is maintained by volunteers on a best-effort basis.  There is no
bug bounty programme.

## Scope

Issues in the following areas are in scope:

- Authentication bypass or session fixation
- Encryption weaknesses or key-material leaks
- Injection vulnerabilities (path traversal, header injection, log forging)
- Unauthorised access to other users' data or admin endpoints
- Denial-of-service vectors exploitable with the default configuration

Out of scope:

- Issues that require the attacker to already hold admin credentials
- Security of a specific third-party deployment — contact that deployment's
  operator directly (see the Privacy Policy / Terms of Service on that
  instance for contact information)
- Social engineering or phishing of individual deployment operators
