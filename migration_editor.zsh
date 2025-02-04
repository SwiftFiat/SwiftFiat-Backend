#!/bin/zsh

# Check if arguments are provided
if [[ -z "$1" || -z "$2" ]]; then
  echo "Usage: $0 <folder_path> <new_file_name> [--revert]"
  exit 1
fi

# Input folder and new file name
dir="$1"
new_file="$2"
revert_flag="$3"

# Ensure the folder exists
if [[ ! -d "$dir" ]]; then
  echo "Error: Folder $dir does not exist!"
  exit 1
fi

# Extract the numeric prefix and base file name
new_file_prefix=$(echo "$new_file" | grep -oE '^[0-9]{6}')
base_file_name=$(echo "$new_file" | sed -E 's/^[0-9]{6}_//')

if [[ -z "$new_file_prefix" ]]; then
  echo "Error: The new file name must start with a 6-digit prefix!"
  exit 1
fi

if [[ "$revert_flag" == "--revert" ]]; then
  # Revert: Remove the specified file and renumber subsequent files
  echo "Reverting changes..."
  rm -f "$dir/${new_file_prefix}_${base_file_name}.up.sql"
  rm -f "$dir/${new_file_prefix}_${base_file_name}.down.sql"
  echo "Removed ${new_file_prefix}_${base_file_name}.up.sql and ${new_file_prefix}_${base_file_name}.down.sql."

  # Renumber files greater than the removed file prefix
  for file in $(ls "$dir" | grep -E '^[0-9]{6}_.+' | sort); do
    file_prefix=$(echo "$file" | grep -oE '^[0-9]{6}')
    if [[ "$file_prefix" -gt "$new_file_prefix" ]]; then
      new_prefix=$(printf "%06d" $((10#$file_prefix - 1)))
      new_name="$new_prefix${file:6}" # Keep the rest of the file name
      mv "$dir/$file" "$dir/$new_name"
    fi
  done
else
  # Add: Renumber files by adding 1 to make space, then insert up/down migrations
  echo "Adding new migration files..."

  # Renumber files greater than or equal to the new file prefix
  for file in $(ls "$dir" | grep -E '^[0-9]{6}_.+' | sort -r); do
    file_prefix=$(echo "$file" | grep -oE '^[0-9]{6}')
    if [[ "$file_prefix" -ge "$new_file_prefix" ]]; then
      new_prefix=$(printf "%06d" $((10#$file_prefix + 1)))
      new_name="$new_prefix${file:6}" # Keep the rest of the file name
      mv "$dir/$file" "$dir/$new_name"
    fi
  done

  # Create the up and down migration files
  touch "$dir/${new_file_prefix}_${base_file_name}.up.sql"
  touch "$dir/${new_file_prefix}_${base_file_name}.down.sql"
  echo "Added ${new_file_prefix}_${base_file_name}.up.sql and ${new_file_prefix}_${base_file_name}.down.sql."
fi


