import newMcpServer from '../server.js';
import { StdioServerTransport } from '@modelcontextprotocol/sdk/server/stdio.js';
const start = async () => {
    const transport = new StdioServerTransport();
    const mcpServer = newMcpServer();
    await mcpServer.connect(transport);
};
export default { start };
