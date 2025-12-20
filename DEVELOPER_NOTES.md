This development specification outlines the requirements for "Archivist", a CLI-based file management utility written in Go. It is designed to automate the movement, organization, and cataloging of files from source directories to a structured archive.
1. Project Overview

Archivist is a Go-based command-line tool that processes files based on a YAML configuration. It moves files into a date- and extension-based directory structure, verifies integrity, maintains a SQLite audit log, and handles naming collisions automatically.
Key Logic Flow
2. Technical Stack

    Language: Go (Golang) 1.21+

    Database: SQLite 3 (via go-sqlite3 or modernc.org/sqlite)

    Configuration: YAML

    Concurrency Control: File-based locking (flock) or PID checking.

3. Configuration & Inputs

The application must support two methods of execution:

    Direct Flags: -input and -output for one-off jobs.

    Config Mode: -config path/to/config.yaml for batch jobs.

3.1 YAML Schema
YAML

jobs:
  - name: "Photos Archive"
    source: "/path/to/raw"
    destination: "/path/to/archive"
  - name: "Documents Archive"
    source: "/users/docs"
    destination: "/nas/docs"

3.2 Ignore Logic

The app must look for an .archiveignore file in the source directory (or a global file via flag). It must support standard glob patterns (e.g., *.tmp, node_modules/, .DS_Store) to skip specific files.
4. Functional Requirements
4.1 Single Instance Enforcement

To prevent data corruption, the app must check for an active process.

    Mechanism: Create a .lock file in the application's working directory upon startup.

    Action: If a lock exists, exit with an error. Remove the lock file on a clean exit or panic recovery.

4.2 Directory Structuring

Files must be moved into the following dynamic path: {destination}/{extension}/{YYYY}/{MM}/{DD}/{filename}

Collision Handling: If {filename} already exists in the destination:

    Attempt to move to: {destination}/_duplicates/{extension}/{YYYY}/{MM}/{DD}/{filename}.

    If it still exists in the _duplicates path, append a 2-digit suffix: filename_01.ext, filename_02.ext, etc.

4.3 Move & Verify Logic

The "Move" must be a Copy-then-Delete operation to ensure safety across different filesystems:

    Copy file to destination.

    Verify integrity (compare File Size or MD5/SHA256 hash).

    If successful: Delete source file and log success.

    If failed: Keep source file, log error, and skip to next file.

5. Database Schema

A SQLite database (archivist.db) must be initialized on first run with two tables:
5.1 Table: history

Logs every action taken by the engine. | Column | Type | Description | | :--- | :--- | :--- | | id | INTEGER PK | Auto-increment | | timestamp | DATETIME | When the event occurred | | job_name | TEXT | Name from config or "manual" | | status | TEXT | "SUCCESS", "FAILED", "SKIPPED" | | message | TEXT | Error details or "File moved" |
5.2 Table: file_registry

Stores metadata for every file successfully archived. | Column | Type | Description | | :--- | :--- | :--- | | id | INTEGER PK | Auto-increment | | original_path | TEXT | Full path of source | | archive_path | TEXT | Full path of destination | | file_name | TEXT | Final filename (including suffixes) | | size | INTEGER | File size in bytes | | checksum | TEXT | MD5 or SHA256 hash | | mod_time | DATETIME | File's original modification date |
6. Execution Workflow

    Initialize: Check for lock file; open/create SQLite DB.

    Parse: Read flags or YAML config to identify job list.

    Crawl: For each job, walk the source directory (respecting .archiveignore).

    Process:

        Extract file metadata (extension, mod date).

        Determine target path.

        Check for collisions and apply _duplicates or suffix logic.

        Execute Copy -> Verify -> Delete.

        Update history and file_registry tables.

    Cleanup: Close DB connections and remove lock file.

7. Error Handling

    Permissions: Log errors if source files are unreadable or destination is unwritable.

    Interruption: Ensure the database commits per-file so that an interrupted process can be resumed without data loss.

    Validation: Ensure the input directory is not a parent of the output directory to avoid infinite loops.

Next Step: Would you like me to generate the Go project structure and the boilerplate code for the SQLite database initialization?This development specification outlines the requirements for "Archivist", a CLI-based file management utility written in Go. It is designed to automate the movement, organization, and cataloging of files from source directories to a structured archive.
1. Project Overview

Archivist is a Go-based command-line tool that processes files based on a YAML configuration. It moves files into a date- and extension-based directory structure, verifies integrity, maintains a SQLite audit log, and handles naming collisions automatically.
Key Logic Flow
2. Technical Stack

    Language: Go (Golang) 1.21+

    Database: SQLite 3 (via go-sqlite3 or modernc.org/sqlite)

    Configuration: YAML

    Concurrency Control: File-based locking (flock) or PID checking.

3. Configuration & Inputs

The application must support two methods of execution:

    Direct Flags: -input and -output for one-off jobs.

    Config Mode: -config path/to/config.yaml for batch jobs.

3.1 YAML Schema
YAML

jobs:
  - name: "Photos Archive"
    source: "/path/to/raw"
    destination: "/path/to/archive"
  - name: "Documents Archive"
    source: "/users/docs"
    destination: "/nas/docs"

3.2 Ignore Logic

The app must look for an .archiveignore file in the source directory (or a global file via flag). It must support standard glob patterns (e.g., *.tmp, node_modules/, .DS_Store) to skip specific files.
4. Functional Requirements
4.1 Single Instance Enforcement

To prevent data corruption, the app must check for an active process.

    Mechanism: Create a .lock file in the application's working directory upon startup.

    Action: If a lock exists, exit with an error. Remove the lock file on a clean exit or panic recovery.

4.2 Directory Structuring

Files must be moved into the following dynamic path: {destination}/{extension}/{YYYY}/{MM}/{DD}/{filename}

Collision Handling: If {filename} already exists in the destination:

    Attempt to move to: {destination}/_duplicates/{extension}/{YYYY}/{MM}/{DD}/{filename}.

    If it still exists in the _duplicates path, append a 2-digit suffix: filename_01.ext, filename_02.ext, etc.

4.3 Move & Verify Logic

The "Move" must be a Copy-then-Delete operation to ensure safety across different filesystems:

    Copy file to destination.

    Verify integrity (compare File Size or MD5/SHA256 hash).

    If successful: Delete source file and log success.

    If failed: Keep source file, log error, and skip to next file.

5. Database Schema

A SQLite database (archivist.db) must be initialized on first run with two tables:
5.1 Table: history

Logs every action taken by the engine. | Column | Type | Description | | :--- | :--- | :--- | | id | INTEGER PK | Auto-increment | | timestamp | DATETIME | When the event occurred | | job_name | TEXT | Name from config or "manual" | | status | TEXT | "SUCCESS", "FAILED", "SKIPPED" | | message | TEXT | Error details or "File moved" |
5.2 Table: file_registry

Stores metadata for every file successfully archived. | Column | Type | Description | | :--- | :--- | :--- | | id | INTEGER PK | Auto-increment | | original_path | TEXT | Full path of source | | archive_path | TEXT | Full path of destination | | file_name | TEXT | Final filename (including suffixes) | | size | INTEGER | File size in bytes | | checksum | TEXT | MD5 or SHA256 hash | | mod_time | DATETIME | File's original modification date |
6. Execution Workflow

    Initialize: Check for lock file; open/create SQLite DB.

    Parse: Read flags or YAML config to identify job list.

    Crawl: For each job, walk the source directory (respecting .archiveignore).

    Process:

        Extract file metadata (extension, mod date).

        Determine target path.

        Check for collisions and apply _duplicates or suffix logic.

        Execute Copy -> Verify -> Delete.

        Update history and file_registry tables.

    Cleanup: Close DB connections and remove lock file.

7. Error Handling

    Permissions: Log errors if source files are unreadable or destination is unwritable.

    Interruption: Ensure the database commits per-file so that an interrupted process can be resumed without data loss.

    Validation: Ensure the input directory is not a parent of the output directory to avoid infinite loops.