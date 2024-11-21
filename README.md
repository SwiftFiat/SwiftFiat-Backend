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


#### Setting up Environment Variables, Starting Services, and Starting App

To set up the SwiftFiat Backend environment variables, declare SERVER_PORT, and use CompileDaemon for automatic code compilation, follow these steps:

1. **Create an Environment File:**
   - Create a new file named `.env` in the root directory of the project.

2. **Start Docker Services:**
   - ensure you have Docker installed on your local machine
   - run `make s_up` to start services

3. **Install Dependencies:**
   - `go mod tidy`

4. **Start App:**
   - run `make start` to start the app using `CompileDaemon` or `make start_d` to start the app in debug mode using `CompileDaemon` and `Delve Debugger`


By following these steps, you can easily get the services up and running and start yout app.
