const util = require("util");
const { PrismaClient } = require("@prisma/client");
const { ucastToPrisma } = require("@styra/ucast-prisma");
const prisma = new PrismaClient();

// Function to read JSON from stdin
async function getStdinJson() {
  const chunks = [];
  for await (const chunk of process.stdin) {
    chunks.push(chunk);
  }
  const data = Buffer.concat(chunks).toString();
  try {
    return JSON.parse(data);
  } catch (error) {
    throw new Error("Invalid JSON input");
  }
}

async function main() {
  try {
    const filters = await getStdinJson();
    const where = ucastToPrisma(filters, "fruit", {
      fruits: { $self: "fruit" },
    });
    console.error(util.inspect(where, { depth: null }));

    const results = await prisma.fruit.findMany({ where });
    process.stdout.write(JSON.stringify(results, null, 2));
  } catch (error) {
    console.error("Error:", error.message);
    process.exit(1);
  } finally {
    await prisma.$disconnect();
  }
}

// Run the script
main();
