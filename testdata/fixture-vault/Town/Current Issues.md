## Open

```dataview
TABLE WITHOUT ID file.link AS Issue, board AS Board, last_updated AS "Last updated", length(Involved) AS People
FROM "Town/Issues"
WHERE status = "open"
SORT last_updated DESC
```

## Resolved

```dataview
TABLE WITHOUT ID file.link AS Issue, board AS Board, last_updated AS "Last updated", length(Involved) AS People
FROM "Town/Issues"
WHERE status = "resolved"
SORT last_updated DESC
```

## All issues by status

```dataview
TABLE WITHOUT ID status AS Status, length(rows) AS Count, rows.file.link AS Issues
FROM "Town/Issues"
WHERE status
GROUP BY status
SORT status ASC
```

## People involved

```dataview
TABLE person AS Person
FROM "Town/Issues"
FLATTEN Involved AS person
WHERE person
```
