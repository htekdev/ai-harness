# Python Conventions — Context Source

This context is injected when Python files are present in the active working
set (`"*.py" in ctx.get("active_files", [])`).

## Python Style Conventions

- Follow **PEP 8** for formatting; use `black` or `ruff` for automatic
  formatting.
- Follow **PEP 257** for docstrings (triple-double-quote, imperative mood).
- Use **type annotations** on all public function signatures (PEP 484 / 526).
- Prefer `pathlib.Path` over `os.path` for filesystem operations.
- Use `dataclasses` or `pydantic` for data-holding types instead of plain dicts.

## Error Handling

- Raise specific exception types; avoid bare `except:` clauses.
- Use `contextlib.suppress` only for truly ignorable errors.
- Always log or re-raise after catching unexpected exceptions.

## Testing

- Tests live in a `tests/` directory mirroring the package structure.
- Use `pytest`; fixtures over setUp/tearDown.
- Parametrize test cases with `@pytest.mark.parametrize`.
- Aim for 80 %+ branch coverage on public APIs.

## Imports

- Standard library → third-party → local (one blank line between groups).
- Use absolute imports; avoid `from module import *`.
