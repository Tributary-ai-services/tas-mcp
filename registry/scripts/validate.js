#!/usr/bin/env node

/**
 * Registry Validation Script
 * Validates MCP server registry against JSON schema
 */

const fs = require('fs');
const path = require('path');
const Ajv = require('ajv');
const addFormats = require('ajv-formats');

const REGISTRY_FILE = path.join(__dirname, '..', 'mcp-servers.json');
const SCHEMA_FILE = path.join(__dirname, '..', 'schema.json');

function validateRegistry() {
  try {
    // Load schema and registry
    const schema = JSON.parse(fs.readFileSync(SCHEMA_FILE, 'utf8'));
    const registry = JSON.parse(fs.readFileSync(REGISTRY_FILE, 'utf8'));
    
    // Create validator
    const ajv = new Ajv({ allErrors: true });
    addFormats(ajv);
    
    const validate = ajv.compile(schema);
    const valid = validate(registry);
    
    if (valid) {
      console.log('✅ Registry validation passed!');
      
      // Additional checks
      const stats = analyzeRegistry(registry);
      console.log('\n📊 Registry Statistics:');
      console.log(`- Total servers: ${stats.total}`);
      console.log(`- Categories: ${Object.keys(stats.byCategory).length}`);
      console.log(`- Free servers: ${stats.byAccess.free || 0}`);
      console.log(`- Servers with endpoints: ${stats.withEndpoints}`);
      
      return true;
    } else {
      console.error('❌ Registry validation failed:');
      validate.errors.forEach(error => {
        console.error(`  - ${error.instancePath}: ${error.message}`);
      });
      return false;
    }
  } catch (error) {
    console.error('❌ Error validating registry:', error.message);
    return false;
  }
}

function analyzeRegistry(registry) {
  const stats = {
    total: registry.servers.length,
    byCategory: {},
    byAccess: {},
    byTransport: {},
    withEndpoints: 0,
    withKubernetes: 0
  };
  
  registry.servers.forEach(server => {
    // Category stats
    stats.byCategory[server.category] = (stats.byCategory[server.category] || 0) + 1;
    
    // Access stats
    stats.byAccess[server.access.type] = (stats.byAccess[server.access.type] || 0) + 1;
    
    // Transport stats
    server.protocols.transport.forEach(transport => {
      stats.byTransport[transport] = (stats.byTransport[transport] || 0) + 1;
    });
    
    // Endpoint stats
    if (server.endpoints && (server.endpoints.http || server.endpoints.grpc || server.endpoints.websocket)) {
      stats.withEndpoints++;
    }
    
    // Kubernetes stats
    if (server.deployment && server.deployment.kubernetes) {
      stats.withKubernetes++;
    }
  });
  
  return stats;
}

function checkDuplicateIds(registry) {
  const ids = new Set();
  const duplicates = [];
  
  registry.servers.forEach(server => {
    if (ids.has(server.id)) {
      duplicates.push(server.id);
    } else {
      ids.add(server.id);
    }
  });
  
  if (duplicates.length > 0) {
    console.error('❌ Duplicate server IDs found:', duplicates);
    return false;
  }
  
  return true;
}

// Main execution
if (require.main === module) {
  const registry = JSON.parse(fs.readFileSync(REGISTRY_FILE, 'utf8'));
  
  const validSchema = validateRegistry();
  const uniqueIds = checkDuplicateIds(registry);
  
  if (validSchema && uniqueIds) {
    console.log('\n✅ All validation checks passed!');
    process.exit(0);
  } else {
    console.log('\n❌ Validation failed!');
    process.exit(1);
  }
}

module.exports = { validateRegistry, analyzeRegistry };