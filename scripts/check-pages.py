"""Check that GitHub Pages includes every locally referenced asset and fragment."""

import re
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlsplit


class Page(HTMLParser):
    def __init__(self, source):
        super().__init__()
        self.ids = set()
        self.references = []
        self.feed(source)

    def handle_starttag(self, tag, attrs):
        attributes = dict(attrs)
        if attributes.get("id"):
            self.ids.add(attributes["id"])
        for name in ("href", "src"):
            if name in attributes:
                self.references.append(attributes[name])


def main():
    site = (Path(__file__).resolve().parents[1] / "site").resolve()
    if not (site / "index.html").is_file():
        raise SystemExit("Missing site/index.html")

    pages = {path: Page(path.read_text(encoding="utf-8")) for path in site.rglob("*.html")}
    references = {path: page.references for path, page in pages.items()}
    for path in site.rglob("*.css"):
        references[path] = re.findall(r"""url\(\s*["']?([^"'\s)]+)""", path.read_text(encoding="utf-8"))

    errors = []
    for source, links in references.items():
        for link in links:
            parts = urlsplit(link)
            if parts.scheme or parts.netloc:
                continue
            target = (source.parent / unquote(parts.path)).resolve() if parts.path else source
            if target.is_dir():
                target /= "index.html"
            if not target.is_relative_to(site) or not target.is_file():
                errors.append(f"{source.relative_to(site)}: missing site-relative target {link}")
            elif parts.fragment and target in pages and unquote(parts.fragment) not in pages[target].ids:
                errors.append(f"{source.relative_to(site)}: missing fragment {link}")

    if errors:
        raise SystemExit("\n".join(errors))
    print(f"Validated local links and assets for {len(pages)} page(s).")


if __name__ == "__main__":
    main()
