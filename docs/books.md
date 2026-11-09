# Books

The first-party catalog supports owner-initiated book discovery and a private request list. Movies and television remain managed by Seerr.

## Discovery

The Go catalog queries Open Library over HTTPS for work identifiers, titles, authors, first publication years and edition counts. It returns up to 12 works from a response capped at 1 MiB. Queries are at most 160 characters, cached for 15 minutes and paced at no more than one upstream request per second. The client does not follow redirects or use HTTP proxies.

Open Library asks third-party applications to identify themselves with a contact address. `services.homelabPlatform.books.contactFile` selects a protected file containing the operator's email. NixOS loads it as a systemd credential. Without this option the catalog is not enabled. Do not store the contact address, passwords or provider credentials in public Git. No background bulk indexing occurs.

## Requests

The private personal state stores a canonical Open Library work ID, title, authors, preferred format (ebook, print or audiobook), owner-recorded status and optimistic revision. A duplicate active work-format request is rejected. A pending request can be marked obtained or cancelled only with the current revision.

These requests are neither automatic purchases nor download instructions. They do not acquire copyrighted works, place library holds, or silently invoke an external provider. Any acquisition must use a separately authorized lawful channel.

## Provider-managed reading and borrowing

An ISBN-specific lookup uses the [Open Library Read API](https://openlibrary.org/dev/docs/api/read) to identify online-readable or borrowable editions. The operation performs one rate-limited, bounded, authenticated-to-Eminence read request per explicit ISBN-10 or ISBN-13. Open Library may report `full access`, `lendable`, `checked out` or `restricted`. An HTTPS reading/borrowing link is returned only for full-access or lendable results, and only if its host is the official Open Library or Internet Archive domain. Legacy HTTP links from those same hosts are upgraded to HTTPS. Other supplied links are discarded.

An availability lookup is not a loan, hold, purchase or download. The owner chooses whether to open the link and authenticates directly on the provider's website. Library entitlements, geographic eligibility, loan limits, changing status and availability are determined exclusively by the provider. Eminence does not ask for, store or use Open Library account credentials, and it does not modify the original personal request ledger.

The lookup is available as `book_lending` in MCP, `GET /v1/books/lending?isbn=...` in the native API, and an ISBN inquiry in the Rust Books page. The provider's legacy Read API can be discontinued; its failure is returned as unavailable rather than claiming that a book can be borrowed.

## Native routes

- `GET /v1/books/search?q=...`: optional on-demand catalog lookup.
- `GET /v1/books/lending?isbn=...`: legal reading/loan availability and verified provider-owned links for an exact ISBN.
- `GET /v1/books/requests?status=...`: owner requests.
- `POST /v1/books/requests`: store a book request.
- `PUT /v1/books/requests/{id}`: explicit revision-guarded status change.

The Rust Books page supports discovery, format selection, request review and status updates. Owner browser writes require authenticated sessions, exact Origin enforcement and CSRF protection. MCP tools remain separately authenticated.
