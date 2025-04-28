# elora-chat 🐐

What if we were fauns? Haha. Just curious man, you don't have to get mad. Just look at that- _gets put in a chokehold_

![Elora](https://static.wikia.nocookie.net/spyro/images/a/a6/Elora_PS1.jpg/revision/latest?cb=20180824195930)

## Description 📝

elora-chat is a versatile chat application designed to unify the streaming experience across multiple platforms. It aims to simplify the chat and alert management for streamers like [Dayoman](https://www.twitch.tv/dayoman) who juggle various services and bots during their streams.

## Why? 🤔

On 1/22/24, [Dayoman](https://twitch.tv/dayoman) expressed the need for a streamlined solution to manage chats and alerts during his streams. He wished to move away from unreliable bots and desired a human touch to his alert systems. Our motivation is to enhance audience interaction and provide a seamless viewing experience across platforms, setting a new standard for multi-stream chats.

elora-chat aims to:

- Reduce the reliance on multiple bots and services.

- Offer a single, human-supported chat system for multiple streaming platforms.

- Enhance the chat experience, ensuring contributions are seen, heard, and appreciated.

- Drive audience engagement, encouraging viewers to participate actively on their preferred networks.

Inspired by pioneers like DougDoug, elora-chat aspires to revolutionize chat interaction while adhering to platform terms of service, ensuring a future-proof solution.

## Quick Start ➡️

- Clone the repository: `git clone https://github.com/hpwn/EloraChat.git`

- Navigate to the project directory: `cd EloraChat`

- Ensure [Docker](https://docs.docker.com/get-started/get-docker/) and [Docker Compose](https://docs.docker.com/compose/install/linux/) are installed and configured.

- Create environment variables: `echo "REDIS_ADDR=redis:6379\nREDIS_PASSWORD=\nTWITCH_CLIENT_ID=\nTWITCH_CLIENT_SECRET=\nTWITCH_REDIRECT_URL=\nYOUTUBE_API_KEY=\nPORT=8080\nDEPLOYED_URL=https://localhost:8080/" > .env`

- Start the server: `docker compose up`

- Connect with your broswer to [http://localhost:8080/](http://localhost:8080/)!

## Usage ⌨️

elora-chat is easy to use. Simply start the server and connect your streaming platforms. The chat will be unified and available in your dashboard for a seamless streaming experience.

## Contributing 🧑🏼‍💻

If you have ideas for improvement or want to contribute to elora-chat, feel free to create a pull request or contact Hayden for collaboration.

Happy streaming! 🎮📹👾

## License

This project is licensed under the **Business Source License 1.1 (BUSL-1.1)**.  
- Non-commercial use only without prior permission.
- Commercial licensing available — [contact](mailto:hwp@arizona.edu) for inquiries.
- On April 25, 2028, the license will convert to Apache 2.0 automatically.

See [LICENSE](./LICENSE) and [COMMERCIAL_LICENSE.md](./COMMERCIAL_LICENSE.md) for more details.
