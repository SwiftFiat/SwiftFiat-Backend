### SwiftFiat Backend

#### Folder Structure

API - Api related calls and grouping - responses etc.
Services - Feature based functions - Business Logic
Providers - Third party functionality
DB - Database LOGIC
Models - Public Structs for Standard Use
Utils - Public utility functions
Templates - Visual templates for messaging (emails, push, sms)

NOTE: The current folder structure may not be EXACTLY this way, but it should eventually be restructured to look this way
NB: Sub-folders are not accounted for in this overview structure


#### Setting up Environment Variables, Declaring SERVER_PORT, and Using CompileDaemon

To set up the SwiftFiat Backend environment variables, declare SERVER_PORT, and use CompileDaemon for automatic code compilation, follow these steps:

1. **Create an Environment File:**
   - Create a new file named `.env` in the root directory of the project.

2. **Declare SERVER_PORT:**
   - Within the `.env` file, declare the SERVER_PORT variable. This variable will define the port on which the server will listen for incoming connections.

     ```
      # ACCESS
      SERVER_PORT=9000

      # AUTHENTICATION - Credentials
      TOKEN_TTL="1800"
      SIGNING_KEY=<32-Bit-Length>

      # DATABASE INFORMATION
      DB_USERNAME=
      DB_PASSWORD=
      DB_HOST=
      DB_NAME=
      DB_PORT=
      DB_DRIVER=postgres

      # ADMINISTRATION - Default Admin User
      ADMIN_USERNAME="<<ADMIN_USERNAME>>"
      ADMIN_EMAIL="<<ADMIN_EMAIL>>"
      ADMIN_PASSWORD="<<ADMIN_PASSWORD>>"

      # AWS KEYS
      AWS_SECRET_ACCESS_KEY=
      AWS_ACCESS_KEY=
      AWS_REGION=
     ```

   Replace `VALUES` with the desired  `INPUTS`

3. **Install CompileDaemon:**
   - Install CompileDaemon using the following command:

     ```
     go get github.com/githubnemo/CompileDaemon
     ```

4. **Run CompileDaemon:**
   - Run CompileDaemon with the command:

     ```
     CompileDaemon -command="./your-executable"
     ```

   Replace `./your-executable` with the command to execute your SwiftFiat Backend server. For example:

     ```
     CompileDaemon -command="./swiftfiat-backend"
     ```

   This command will continuously monitor your project files for changes and automatically recompile and restart your server whenever a change is detected.

5. **Usage:**
   - The declared SERVER_PORT environment variable will be used by the SwiftFiat Backend to determine the port for hosting the server.

By following these steps, you can easily set up the necessary environment variables, declare the SERVER_PORT, and use CompileDaemon for automatic code compilation in the SwiftFiat Backend project.
