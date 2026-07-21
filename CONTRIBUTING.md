# Contributing to Dify2API

Thank you for considering a contribution!  This document explains how to get
started and what to expect.

## Licence

Dify2API is licensed under the **GNU Affero General Public License v3.0
(AGPL-3.0)**.  By submitting a contribution you agree to license your work
under the same terms.  See [LICENSE](LICENSE) for the full text.

> If you are contributing on behalf of an employer, make sure you have the
> right to do so under your employment agreement or open-source policy.

The project maintainer also offers Dify2API under alternative terms for
closed-source commercial use (see [README](README.md) §许可证).  If you are
comfortable with the maintainer re-licensing your contribution for that
purpose, no additional action is needed.  If you would prefer your
contribution to remain **AGPL-only**, please state that explicitly in your
pull request.

## Developer Certificate of Origin

All commits must include a `Signed-off-by` line certifying that you wrote the
code and have the right to contribute it under the project's licence:

```
Signed-off-by: Your Name <your@email.example>
```

You can add this automatically with `git commit -s`.

This is the [Developer Certificate of Origin (DCO)](https://developercertificate.org/)
— a lightweight alternative to a Contributor Licence Agreement.

## How to Contribute

1. **Discuss first** for anything larger than a typo fix.  Open an issue
   describing what you want to do and why; the maintainer will give feedback
   before you invest significant time.

2. **Fork & branch** — create a feature branch from `master`.

3. **Follow the project conventions** described in `AGENTS.md` §4 (service
   registry, contract validation, error format, API JSON tags, DB migrations).

4. **Write tests** for new functionality.  Run the full suite before pushing:

   ```bash
   go build ./... && go vet ./... && go test -count=1 ./...
   ```

5. **One logical change per PR.**  Keep commits small and focused; squash
   fix-up commits before requesting review.

6. **Update documentation** if your change affects user-visible behaviour
   (README, `admin.env.example`, inline help, etc.).

## Code Style

- `go fmt` is non-negotiable — CI will reject unformatted code.
- Comments and user-facing strings are in **Chinese** (简体中文) by
  convention, but English is accepted for code identifiers and technical
  documentation.
- New error codes must be added to the table in `README.md` §错误列表.

## Review Process

The maintainer reviews PRs on a best-effort basis.  To help move things along:

- Keep PRs small and focused.
- Link to the issue you are addressing.
- Make sure CI passes (build + vet + test).
- Respond to review comments promptly.

## Questions?

Open a [GitHub Discussion](https://github.com) or email the project maintainer
at the address shown in the repository.
