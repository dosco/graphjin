# How OpenNeko is licensed

The source code in this repository is licensed under the
[Apache License 2.0](LICENSE), unless a file clearly states different terms.

The Apache License 2.0 permits use, modification, distribution, commercial use,
and hosted use, subject to its terms. It also requires preservation of required
copyright, patent, trademark, attribution, license, and NOTICE materials in
distributed copies and derivative works.

The Apache License 2.0 does **not** grant rights to use the OpenNeko name, logo,
or branding. Those marks are governed separately by [TRADEMARKS.md](TRADEMARKS.md).

## Copyright and attribution

Original OpenNeko code authored by Amit Deshmukh is copyrighted by Amit
Deshmukh. Contributor-authored code remains owned by its respective copyright
holders and is licensed under the applicable contribution terms. See
[NOTICE](NOTICE) and [CLA.md](CLA.md).

## Third-party and modular components

OpenNeko depends on and can interoperate with software that is not owned by
OpenNeko. Those components remain under their own licenses.

Examples include:

- Runtime dependencies and container-layer software listed in
  [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md).
- Built-in skills adapted from Nous Research's agent skill catalog, under its
  upstream MIT license (see [THIRD_PARTY_LICENSES.md](THIRD_PARTY_LICENSES.md)).
- Anthropic-authored bundled skills under their declared upstream terms.
- Built-in skills that carry their own `license:` frontmatter or local
  `LICENSE.txt` files.
- Third-party plugins that communicate with OpenNeko across the plugin RPC
  boundary.

If an individual file, package, bundled skill, plugin, or dependency includes a
different license notice, that notice controls that component.

## Plugins are independent works

OpenNeko loads plugins as separate processes inside sandboxes, across a defined
RPC boundary. A third-party plugin that communicates with OpenNeko only across
that boundary is an independent work. Plugin authors may license their plugins
however they choose.

OpenNeko plugin interfaces and in-repository plugin support code are covered by
the repository license unless a file says otherwise.

## Trademarks and hosted services

"OpenNeko" and the OpenNeko logo are trademarks of Amit Deshmukh / OpenNeko.
Apache 2.0 does not grant trademark rights.

Hosted services, forks, and commercial offerings based on OpenNeko must comply
with the Apache 2.0 license and NOTICE obligations. They must also follow the
OpenNeko trademark policy and use their own branding unless they have written
permission to use OpenNeko marks.

See [TRADEMARKS.md](TRADEMARKS.md).

## Contributing

Contributions are welcome under the [Contributor License Agreement](CLA.md).
The CLA lets contributors keep ownership of their contributions while granting
the project the rights needed to license OpenNeko under Apache 2.0 and relicense
if necessary. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Services

Support, certification, partnership, and trademark permissions may be offered
separately.
