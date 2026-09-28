import {SemanticDesktop} from './semantic-driver.mjs';
const desktop=new SemanticDesktop();
try {await desktop.bindFixture();console.log(JSON.stringify(await desktop.observe(),null,2));}
finally {await desktop.close();}
